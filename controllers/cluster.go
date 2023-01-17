package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	deprecatedControlPlaneLabel = "node-role.kubernetes.io/master"
	controlPlaneLabel           = "node-role.kubernetes.io/control-plane"
)

// IsControlPlaneReady takes a client connected to a cluster and reports whether or
// not the control-plane for the cluster is "ready".
func IsControlPlaneReady(ctx context.Context, cl client.Client) (bool, error) {
	logger := log.FromContext(ctx)
	readiness := []bool{}
	readyNodes, err := listReadyNodesWithLabel(ctx, logger, cl, controlPlaneLabel)
	if err != nil {
		return false, err
	}
	readiness = append(readiness, readyNodes...)

	if len(readyNodes) == 0 {
		readyNodes, err := listReadyNodesWithLabel(ctx, logger, cl, deprecatedControlPlaneLabel)
		if err != nil {
			return false, err
		}
		readiness = append(readiness, readyNodes...)
	}

	isReady := func(bools []bool) bool {
		for _, v := range bools {
			if !v {
				return false
			}
		}
		return true
	}
	logger.Info("readiness", "len", len(readiness), "is-ready", isReady(readiness))

	// If we have no statuses, then we really don't know if we're ready or not.
	return (len(readiness) > 0 && isReady(readiness)), nil
}

func listReadyNodesWithLabel(ctx context.Context, logger logr.Logger, cl client.Client, label string) ([]bool, error) {
	nodes := &corev1.NodeList{}
	// https://github.com/kubernetes/enhancements/blob/master/keps/sig-cluster-lifecycle/kubeadm/2067-rename-master-label-taint/README.md#design-details
	err := cl.List(ctx, nodes, client.HasLabels([]string{label}))
	if err != nil {
		return nil, fmt.Errorf("failed to query cluster node list: %w", err)
	}
	logger.Info("listed nodes with control plane label", "label", label, "count", len(nodes.Items))

	readiness := []bool{}
	for _, node := range nodes.Items {
		for _, c := range node.Status.Conditions {
			switch c.Type {
			case corev1.NodeReady:
				readiness = append(readiness, c.Status == corev1.ConditionTrue)
			}
		}
	}
	return readiness, nil
}

func matchCluster(cluster *gitopsv1alpha1.GitopsCluster, labelSelector metav1.LabelSelector) bool {
	selector, err := metav1.LabelSelectorAsSelector(&labelSelector)
	if err != nil {
		return false
	}
	labelSet := labels.Set(cluster.GetLabels())
	if selector.Empty() {
		return false
	}
	return selector.Matches(labelSet)
}

func clientForCluster(ctx context.Context, cl client.Client, configParser ConfigParser, name types.NamespacedName) (client.Client, error) {
	kubeConfigBytes, err := getKubeConfig(ctx, cl, name)
	if err != nil {
		return nil, err
	}

	client, err := configParser(kubeConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("getting client for cluster %s: %w", name, err)
	}
	return client, nil
}

func getKubeConfig(ctx context.Context, cl client.Client, cluster types.NamespacedName) ([]byte, error) {
	secretName := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name + "-kubeconfig",
	}

	var secret corev1.Secret
	if err := cl.Get(ctx, secretName, &secret); err != nil {
		return nil, fmt.Errorf("unable to read KubeConfig secret %q error: %w", secretName, err)
	}

	var kubeConfig []byte
	for k := range secret.Data {
		if k == "value" || k == "value.yaml" {
			kubeConfig = secret.Data[k]
			break
		}
	}

	if len(kubeConfig) == 0 {
		return nil, fmt.Errorf("KubeConfig secret %q doesn't contain a 'value' key ", secretName)
	}

	return kubeConfig, nil
}

func kubeConfigBytesToClient(b []byte) (client.Client, error) {
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(b)
	if err != nil {
		return nil, fmt.Errorf("failed to parse KubeConfig from secret: %w", err)
	}
	restMapper, err := apiutil.NewDynamicRESTMapper(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create RESTMapper from config: %w", err)
	}

	client, err := client.New(restConfig, client.Options{Mapper: restMapper})
	if err != nil {
		return nil, fmt.Errorf("failed to create a client from config: %w", err)
	}
	return client, nil
}
