package controllers

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
)

func TestReconcile_when_cluster_not_ready(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	notReadyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{Type: "Ready", Status: "False", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"})

	cl := makeTestCluster(func(c *clusterv1.Cluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Phase = string(clusterv1.ClusterPhaseProvisioned)
	})
	secret := makeTestSecret(types.NamespacedName{
		Name:      cl.GetName() + "-kubeconfig",
		Namespace: cl.GetNamespace(),
	}, map[string][]byte{"value": []byte("testing")})
	reconciler := makeTestReconciler(t, bc, cl, secret, notReadyNode)

	// This cheats by using the test client as the remote client to simplify
	// getting the value from the remote client.
	reconciler.configParser = func(b []byte) (client.Client, error) {
		return reconciler.Client, nil
	}

	result, err := reconciler.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
		Name:      bc.GetName(),
		Namespace: bc.GetNamespace(),
	}})
	if err != nil {
		t.Fatal(err)
	}

	if result.RequeueAfter != requeueAfterTime {
		t.Fatalf("RequeueAfter got %v, want %v", result.RequeueAfter, requeueAfterTime)
	}
	var jobs batchv1.JobList
	if err := reconciler.List(context.TODO(), &jobs, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}
	if l := len(jobs.Items); l != 0 {
		t.Fatalf("found %d jobs, want %d", l, 0)
	}

}

func TestReconcile_when_cluster_ready(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"})

	cl := makeTestCluster(func(c *clusterv1.Cluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Phase = string(clusterv1.ClusterPhaseProvisioned)
	})
	secret := makeTestSecret(types.NamespacedName{
		Name:      cl.GetName() + "-kubeconfig",
		Namespace: cl.GetNamespace(),
	}, map[string][]byte{"value": []byte("testing")})
	// This cheats by using the local client as the remote client to simplify
	// getting the value from the remote client.
	reconciler := makeTestReconciler(t, bc, cl, secret, readyNode)
	reconciler.configParser = func(b []byte) (client.Client, error) {
		return reconciler.Client, nil
	}

	result, err := reconciler.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{
		Name:      bc.GetName(),
		Namespace: bc.GetNamespace(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsZero() {
		t.Fatalf("want empty result, got %v", result)
	}
	var jobs batchv1.JobList
	if err := reconciler.List(context.TODO(), &jobs, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}
	if l := len(jobs.Items); l != 1 {
		t.Fatalf("found %d jobs, want %d", l, 1)
	}
}

func makeTestReconciler(t *testing.T, objs ...runtime.Object) *ClusterBootstrapConfigReconciler {
	s, tc := makeTestClientAndScheme(t, objs...)
	return NewClusterBootstrapConfigReconciler(tc, s)
}

func makeTestSecret(name types.NamespacedName, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: name.Namespace,
			Name:      name.Name,
		},
		Data: data,
	}
}
