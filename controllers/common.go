package controllers

import (
	"context"
	"fmt"

	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type ConfigParser func(b []byte) (client.Client, error)

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
