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
	"fmt"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha2"
	capiv1alpha2 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha2"
)

const (
	secretRefIndexKey       = "spec.secretRef"
	clusterReadinessRequeue = time.Duration(1 * time.Minute)
)

// SecretSyncReconciler reconciles a SecretSync object
type SecretSyncReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	configParser ConfigParser
}

// NewSecretSyncReconciler creates and returns a configured
// reconciler ready for use.
func NewSecretSyncReconciler(c client.Client, s *runtime.Scheme) *SecretSyncReconciler {
	return &SecretSyncReconciler{
		Client:       c,
		Scheme:       s,
		configParser: kubeConfigBytesToClient,
	}
}

//+kubebuilder:rbac:groups=capi.weave.works,resources=secretsyncs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=capi.weave.works,resources=secretsyncs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=capi.weave.works,resources=secretsyncs/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="gitops.weave.works",resources=gitopsclusters,verbs=get;watch;list;patch

func (r *SecretSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var secretSync capiv1alpha2.SecretSync
	if err := r.Client.Get(ctx, req.NamespacedName, &secretSync); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	secretName := types.NamespacedName{
		Name:      secretSync.Spec.SecretRef.Name,
		Namespace: req.Namespace,
	}
	var secret corev1.Secret
	if err := r.Get(ctx, secretName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !secret.DeletionTimestamp.IsZero() {
		logger.Info("skipping secret", "name", secret.Name, "namespace", secret.Namespace, "reason", "Deleted")
		return ctrl.Result{}, nil
	}

	selector, err := metav1.LabelSelectorAsSelector(&secretSync.Spec.ClusterSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to convert selector: %w", err)
	}

	if selector.Empty() {
		logger.Info("empty cluster selector: no clusters are selected")
		return ctrl.Result{}, nil
	}

	clusters := &gitopsv1alpha1.GitopsClusterList{}
	if err := r.Client.List(ctx, clusters, client.InNamespace(req.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list clusters: %w", err)
	}

	var requeue bool

	for i := range clusters.Items {
		cluster := clusters.Items[i]

		if !cluster.DeletionTimestamp.IsZero() {
			logger.Info("skipping cluster", "cluster", cluster.Name, "namespace", req.Namespace, "reason", "Deleted")
			continue
		}

		if !conditions.IsReady(&cluster) {
			logger.Info("skipping cluster", "cluster", cluster.Name, "namespace", req.Namespace, "reason", "NotReady")
			continue
		}

		clusterName := types.NamespacedName{Name: cluster.GetName(), Namespace: cluster.GetNamespace()}
		clusterClient, err := clientForCluster(ctx, r.Client, r.configParser, clusterName)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info("waiting for cluster access secret to be available", "cluster", cluster.Name, "namespace", req.Namespace)
				requeue = true
				continue
			}
			return ctrl.Result{}, fmt.Errorf("failed to create client for cluster %s: %w", clusterName, err)
		}

		ready, err := IsControlPlaneReady(ctx, clusterClient)
		if err != nil {
			logger.Error(err, "failed to check readiness of cluster", "cluster", cluster.Name, "namespace", req.Namespace)
			continue
		}

		if !ready {
			logger.Info("waiting for control plane to be ready", "cluster", cluster.Name, "namespace", req.Namespace)
			requeue = true
			continue
		}

		if err := r.sync(ctx, secret, clusterClient); err != nil {
			logger.Error(err, "failed to sync secret", "cluster", cluster.Name, "secret", secret.Name, "namespace", req.Namespace)
			continue
		}
	}

	if requeue {
		return ctrl.Result{RequeueAfter: clusterReadinessRequeue}, nil
	}
	return ctrl.Result{}, nil
}

func (r *SecretSyncReconciler) sync(ctx context.Context, secret v1.Secret, cl client.Client) error {
	newSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:         secret.Name,
			GenerateName: secret.GenerateName,
			Namespace:    secret.Namespace,
			Labels:       secret.Labels,
			Annotations:  secret.Annotations,
		},
		Type:      secret.Type,
		Immutable: secret.Immutable,
		Data:      secret.Data,
	}

	if err := cl.Create(ctx, &newSecret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			if err := cl.Update(ctx, &newSecret); err != nil {
				return fmt.Errorf("failed to update secret %w", err)
			}
		} else {
			return fmt.Errorf("failed to create secret %w", err)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha2.SecretSync{}, secretRefIndexKey, func(obj client.Object) []string {
		secretSync, ok := obj.(*v1alpha2.SecretSync)
		if !ok {
			return nil
		}
		return []string{secretSync.Spec.SecretRef.Name}
	})

	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&capiv1alpha2.SecretSync{}).
		Watches(
			&source.Kind{Type: &gitopsv1alpha1.GitopsCluster{}},
			handler.EnqueueRequestsFromMapFunc(r.clusterHandler),
		).
		Watches(
			&source.Kind{Type: &v1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.secretHandler),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *SecretSyncReconciler) clusterHandler(obj client.Object) []ctrl.Request {
	cluster, ok := obj.(*gitopsv1alpha1.GitopsCluster)
	if !ok {
		return nil
	}

	var resources capiv1alpha2.SecretSyncList
	if err := r.Client.List(context.Background(), &resources, client.InNamespace(cluster.Namespace)); err != nil {
		return nil
	}

	result := []ctrl.Request{}
	for _, resource := range resources.Items {
		if !matchCluster(cluster, resource.Spec.ClusterSelector) {
			continue
		}
		result = append(result, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      resource.Name,
				Namespace: resource.Namespace,
			},
		})
	}
	return result
}

func (r *SecretSyncReconciler) secretHandler(obj client.Object) []ctrl.Request {
	opts := client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(secretRefIndexKey, obj.GetName()),
		Namespace:     obj.GetNamespace(),
	}

	var resources v1alpha2.SecretSyncList
	ctx := context.Background()
	if err := r.List(ctx, &resources, &opts); err != nil {
		log.Log.Error(err, "failed to list secret syncs")
		return nil
	}

	var requests []reconcile.Request
	for _, item := range resources.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.Name,
				Namespace: item.Namespace,
			},
		})
	}

	return requests
}
