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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	capiv1alpha1 "github.com/weaveworks-gitops-poc/cluster-bootstrap-controller/api/v1alpha1"
)

// ClusterBootstrapConfigReconciler reconciles a ClusterBootstrapConfig object
type ClusterBootstrapConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=capi.weave.works,resources=clusterbootstrapconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=capi.weave.works,resources=clusterbootstrapconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=capi.weave.works,resources=clusterbootstrapconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *ClusterBootstrapConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var clusterBootstrapConfig capiv1alpha1.ClusterBootstrapConfig
	if err := r.Client.Get(ctx, req.NamespacedName, &clusterBootstrapConfig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("cluster bootstrap config loaded", "name", clusterBootstrapConfig.ObjectMeta.Name)

	clusters, err := r.getClustersBySelector(ctx, req.Namespace, clusterBootstrapConfig.Spec.ClusterSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to getClustersBySelector for bootstrap config %s: %w", req, err)
	}
	logger.Info("identified clusters for reconciliation", "clusterCount", len(clusters))

	for _, c := range clusters {
		if err := bootstrapClusterWithConfig(ctx, logger, r.Client, c, &clusterBootstrapConfig); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to bootstrap cluster config: %w", err)
		}

		mergePatch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					capiv1alpha1.BootstrappedAnnotation: "yes",
				},
			},
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create a patch to update the cluster annotations: %w", err)
		}
		if err := r.Client.Patch(ctx, c, client.RawPatch(types.MergePatchType, mergePatch)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to annotate cluster %s/%s as bootstrapped: %w", c.ObjectMeta.Name, c.ObjectMeta.Namespace, err)
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterBootstrapConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&capiv1alpha1.ClusterBootstrapConfig{}).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			handler.EnqueueRequestsFromMapFunc(r.clusterToClusterBootstrapConfig),
		).
		Complete(r)
}

func (r *ClusterBootstrapConfigReconciler) getClustersBySelector(ctx context.Context, ns string, ls metav1.LabelSelector) ([]*clusterv1.Cluster, error) {
	logger := ctrl.LoggerFrom(ctx)
	selector, err := metav1.LabelSelectorAsSelector(&ls)
	if err != nil {
		return nil, fmt.Errorf("unable to convert selector: %w", err)
	}

	if selector.Empty() {
		logger.Info("empty ClusterBootstrapConfig selector: no clusters are selected")
		return nil, nil
	}
	clusterList := &clusterv1.ClusterList{}
	if err := r.Client.List(ctx, clusterList, client.InNamespace(ns), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}

	logger.Info("identified clusters with selector", "selector", selector, "count", len(clusterList.Items))
	clusters := []*clusterv1.Cluster{}
	for i := range clusterList.Items {
		c := &clusterList.Items[i]
		if clusterv1.ClusterPhase(c.Status.Phase) != clusterv1.ClusterPhaseProvisioned {
			logger.Info("cluster discarded - not provisioned", "phase", c.Status.Phase)
			continue
		}
		if metav1.HasAnnotation(c.ObjectMeta, capiv1alpha1.BootstrappedAnnotation) {
			continue
		}
		if c.DeletionTimestamp.IsZero() {
			clusters = append(clusters, c)
		}
	}
	return clusters, nil
}

// clusterToClusterBootstrapConfig is mapper function that maps clusters to
// ClusterBootstrapConfig.
func (r *ClusterBootstrapConfigReconciler) clusterToClusterBootstrapConfig(o client.Object) []ctrl.Request {
	result := []ctrl.Request{}
	cluster, ok := o.(*clusterv1.Cluster)
	if !ok {
		panic(fmt.Sprintf("Expected a Cluster but got a %T", o))
	}

	resourceList := capiv1alpha1.ClusterBootstrapConfigList{}
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
