/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fluxcd/pkg/runtime/conditions"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	capiv1alpha2 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha2"
)

// ClusterBootstrapConfigReconciler reconciles a ClusterBootstrapConfig object
type ClusterBootstrapConfigReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	configParser func(b []byte) (client.Client, error)
}

// NewClusterBootstrapConfigReconcielr creates and returns a configured
// reconciler ready for use.
func NewClusterBootstrapConfigReconciler(c client.Client, s *runtime.Scheme) *ClusterBootstrapConfigReconciler {
	return &ClusterBootstrapConfigReconciler{
		Client:       c,
		Scheme:       s,
		configParser: kubeConfigBytesToClient,
	}
}

//+kubebuilder:rbac:groups=capi.weave.works,resources=clusterbootstrapconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=capi.weave.works,resources=clusterbootstrapconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=capi.weave.works,resources=clusterbootstrapconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="gitops.weave.works",resources=gitopsclusters,verbs=get;watch;list;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *ClusterBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var clusterBootstrapConfig capiv1alpha2.ClusterBootstrapConfig
	if err := r.Client.Get(ctx, req.NamespacedName, &clusterBootstrapConfig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	logger.Info("cluster bootstrap config loaded", "name", clusterBootstrapConfig.ObjectMeta.Name)

	clusters, err := r.getClustersBySelector(ctx, req.Namespace, clusterBootstrapConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to getClustersBySelector for bootstrap config %s: %w", req, err)
	}
	logger.Info("identified clusters for reconciliation", "clusterCount", len(clusters))

	for _, cluster := range clusters {
		if clusterBootstrapConfig.Spec.RequireClusterReady {
			clusterName := types.NamespacedName{Name: cluster.GetName(), Namespace: cluster.GetNamespace()}
			clusterClient, err := r.clientForCluster(ctx, clusterName)
			if err != nil {
				if apierrors.IsNotFound(err) {
					logger.Info("waiting for cluster access secret to be available")
					return ctrl.Result{RequeueAfter: clusterBootstrapConfig.ClusterReadinessRequeue()}, nil
				}

				return ctrl.Result{}, fmt.Errorf("failed to create client for cluster %s: %w", clusterName, err)
			}

			ready, err := IsControlPlaneReady(ctx, clusterClient)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to check readiness of cluster %s: %w", clusterName, err)
			}
			if !ready {
				logger.Info("waiting for control plane to be ready", "cluster", clusterName)

				return ctrl.Result{RequeueAfter: clusterBootstrapConfig.ClusterReadinessRequeue()}, nil
			}
		}
		if err := bootstrapClusterWithConfig(ctx, logger, r.Client, cluster, &clusterBootstrapConfig); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to bootstrap cluster config: %w", err)
		}

		mergePatch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					capiv1alpha2.BootstrappedAnnotation:     "yes",
					capiv1alpha2.BootstrapConfigsAnnotation: appendClusterConfigToBootstrappedList(clusterBootstrapConfig, cluster),
				},
			},
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create a patch to update the cluster annotations: %w", err)
		}
		if err := r.Client.Patch(ctx, cluster, client.RawPatch(types.MergePatchType, mergePatch)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to annotate cluster %s/%s as bootstrapped: %w", cluster.ObjectMeta.Name, cluster.ObjectMeta.Namespace, err)
		}
	}
	return ctrl.Result{}, nil
}

func appendClusterConfigToBootstrappedList(config capiv1alpha2.ClusterBootstrapConfig, cluster *gitopsv1alpha1.GitopsCluster) string {
	current := cluster.GetAnnotations()[capiv1alpha2.BootstrapConfigsAnnotation]
	set := sets.NewString(strings.Split(current, ",")...)
	id := fmt.Sprintf("%s/%s", config.GetNamespace(), config.GetName())
	set.Insert(id)
	return strings.Join(set.List(), ",")
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterBootstrapConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capiv1alpha2.ClusterBootstrapConfig{}).
		Watches(
			&source.Kind{Type: &gitopsv1alpha1.GitopsCluster{}},
			handler.EnqueueRequestsFromMapFunc(r.clusterToClusterBootstrapConfig),
		).
		Complete(r)
}

func (r *ClusterBootstrapConfigReconciler) getClustersBySelector(ctx context.Context, ns string, config capiv1alpha2.ClusterBootstrapConfig) ([]*gitopsv1alpha1.GitopsCluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	selector, err := metav1.LabelSelectorAsSelector(&config.Spec.ClusterSelector)
	if err != nil {
		return nil, fmt.Errorf("unable to convert selector: %w", err)
	}

	if selector.Empty() {
		logger.Info("empty ClusterBootstrapConfig selector: no clusters are selected")
		return nil, nil
	}
	clusterList := &gitopsv1alpha1.GitopsClusterList{}
	if err := r.Client.List(ctx, clusterList, client.InNamespace(ns), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}

	logger.Info("identified clusters with selector", "selector", selector, "count", len(clusterList.Items))
	clusters := []*gitopsv1alpha1.GitopsCluster{}
	for i := range clusterList.Items {
		cluster := &clusterList.Items[i]

		if !conditions.IsReady(cluster) && !config.Spec.RequireClusterProvisioned {
			logger.Info("cluster discarded - not ready", "phase", cluster.Status)
			continue
		}
		if config.Spec.RequireClusterProvisioned {
			if !isProvisioned(cluster) {
				logger.Info("waiting for cluster to be provisioned", "cluster", cluster.Name)
				continue
			}
		}

		if metav1.HasAnnotation(cluster.ObjectMeta, capiv1alpha2.BootstrappedAnnotation) {
			if alreadyBootstrappedWithConfig(cluster, config) {
				continue
			}
		}
		if cluster.DeletionTimestamp.IsZero() {
			clusters = append(clusters, cluster)
		}
	}
	return clusters, nil
}

func alreadyBootstrappedWithConfig(cluster *gitopsv1alpha1.GitopsCluster, config capiv1alpha2.ClusterBootstrapConfig) bool {
	current := cluster.GetAnnotations()[capiv1alpha2.BootstrapConfigsAnnotation]
	set := sets.NewString(strings.Split(current, ",")...)
	id := fmt.Sprintf("%s/%s", config.GetNamespace(), config.GetName())
	return set.Has(id)
}

// clusterToClusterBootstrapConfig is mapper function that maps clusters to
// ClusterBootstrapConfig.
func (r *ClusterBootstrapConfigReconciler) clusterToClusterBootstrapConfig(o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	cluster, ok := o.(*gitopsv1alpha1.GitopsCluster)
	if !ok {
		panic(fmt.Sprintf("Expected a Cluster but got a %T", o))
	}

	resourceList := capiv1alpha2.ClusterBootstrapConfigList{}
	if err := r.Client.List(context.Background(), &resourceList, client.InNamespace(cluster.Namespace)); err != nil {
		return nil
	}

	labels := labels.Set(cluster.GetLabels())
	for i := range resourceList.Items {
		rs := &resourceList.Items[i]
		selector, err := metav1.LabelSelectorAsSelector(&rs.Spec.ClusterSelector)
		if err != nil {
			return nil
		}

		// If a ClusterResourceSet has a nil or empty selector, it should match nothing, not everything.
		if selector.Empty() {
			return nil
		}

		if !selector.Matches(labels) {
			continue
		}

		name := client.ObjectKey{Namespace: rs.Namespace, Name: rs.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

func (r *ClusterBootstrapConfigReconciler) clientForCluster(ctx context.Context, name types.NamespacedName) (client.Client, error) {
	kubeConfigBytes, err := r.getKubeConfig(ctx, name)
	if err != nil {
		return nil, err
	}

	client, err := r.configParser(kubeConfigBytes)
	if err != nil {
		return nil, fmt.Errorf("getting client for cluster %s: %w", name, err)
	}
	return client, nil
}

func (r *ClusterBootstrapConfigReconciler) getKubeConfig(ctx context.Context, cluster types.NamespacedName) ([]byte, error) {
	secretName := types.NamespacedName{
		Namespace: cluster.Namespace,
		Name:      cluster.Name + "-kubeconfig",
	}

	var secret corev1.Secret
	if err := r.Client.Get(ctx, secretName, &secret); err != nil {
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

func isProvisioned(from conditions.Getter) bool {
	return conditions.IsTrue(from, gitopsv1alpha1.ClusterProvisionedCondition)
}
