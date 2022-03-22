package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-bootstrap-controller/test"
)

const testWaitDuration = time.Second * 55

func TestReconcile_when_cluster_not_ready(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
		c.Spec.ClusterReadinessBackoff = &metav1.Duration{Duration: testWaitDuration}

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

	if result.RequeueAfter != testWaitDuration {
		t.Fatalf("RequeueAfter got %v, want %v", result.RequeueAfter, testWaitDuration)
	}
	assertNoJobsCreated(t, reconciler.Client)
}

func TestReconcile_when_cluster_secret_not_available(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	cl := makeTestCluster(func(c *clusterv1.Cluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Phase = string(clusterv1.ClusterPhaseProvisioned)
	})
	reconciler := makeTestReconciler(t, bc, cl)

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

	// this is the default requeue after time
	if result.RequeueAfter != time.Second*60 {
		t.Fatalf("RequeueAfter got %v, want %v", result.RequeueAfter, time.Second*60)
	}
}

func TestReconcile_when_cluster_ready(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{
		Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"})

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

func TestReconcile_when_cluster_no_matching_labels(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	cl := makeTestCluster(func(c *clusterv1.Cluster) {
		c.ObjectMeta.Labels = map[string]string{
			"will-not-match": "",
		}
		c.Status.Phase = string(clusterv1.ClusterPhaseProvisioned)
	})
	// This cheats by using the local client as the remote client to simplify
	// getting the value from the remote client.
	reconciler := makeTestReconciler(t, bc, cl)
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
	assertNoJobsCreated(t, reconciler.Client)
}

func TestReconcile_when_empty_label_selector(t *testing.T) {
	// When the label selector is empty, we don't want any jobs created, rather
	// than a job for all clusters.
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
		c.Spec.ClusterSelector = metav1.LabelSelector{
			MatchLabels: map[string]string{},
		}

	})
	cl := makeTestCluster(func(c *clusterv1.Cluster) {
		c.ObjectMeta.Labels = map[string]string{
			"will-not-match": "",
		}
		c.Status.Phase = string(clusterv1.ClusterPhaseProvisioned)
	})
	// This cheats by using the local client as the remote client to simplify
	// getting the value from the remote client.
	reconciler := makeTestReconciler(t, bc, cl)
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
	assertNoJobsCreated(t, reconciler.Client)
}

func TestReconcile_when_cluster_ready_and_old_label(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/master": "",
	}, corev1.NodeCondition{Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(),
		LastTransitionTime: metav1.Now(), Reason: "KubeletReady",
		Message: "kubelet is posting ready status"})

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

func Test_kubeConfigBytesToClient_with_valid_kubeconfig(t *testing.T) {
	// Fake the server out.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"apiVersion":"", "kind":""}`)
	}))
	defer ts.Close()

	name := "test-cluster"
	cluster := clientcmdapi.NewCluster()
	cluster.Server = ts.URL

	context := clientcmdapi.NewContext()
	context.Cluster = name

	clientConfig := clientcmdapi.NewConfig()
	clientConfig.Clusters[name] = cluster
	clientConfig.Contexts[name] = context
	clientConfig.CurrentContext = name

	b, err := clientcmd.Write(*clientConfig)
	if err != nil {
		t.Fatal(err)
	}

	_, err = kubeConfigBytesToClient(b)
	if err != nil {
		t.Fatal(err)
	}
}

func Test_kubeConfigBytesToClient_with_bad_kubeconfig_server(t *testing.T) {
	// Fake the server out.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `testing`)
	}))
	defer ts.Close()

	name := "test-cluster"
	cluster := clientcmdapi.NewCluster()
	cluster.Server = ts.URL

	context := clientcmdapi.NewContext()
	context.Cluster = name

	clientConfig := clientcmdapi.NewConfig()
	clientConfig.Clusters[name] = cluster
	clientConfig.Contexts[name] = context
	clientConfig.CurrentContext = name

	b, err := clientcmd.Write(*clientConfig)
	if err != nil {
		t.Fatal(err)
	}

	_, err = kubeConfigBytesToClient(b)
	test.AssertErrorMatch(t, "failed to create RESTMapper from config: couldn't get version/kind", err)
}

func Test_kubeConfigBytesToClient_with_invalidkubeconfig(t *testing.T) {
	_, err := kubeConfigBytesToClient([]byte("testing"))
	if err == nil {
		t.Fatal("expected to get an error parsing an invalid kubeconfig secret")
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

func assertNoJobsCreated(t *testing.T, cl client.Client) {
	var jobs batchv1.JobList
	if err := cl.List(context.TODO(), &jobs, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}
	if l := len(jobs.Items); l != 0 {
		t.Fatalf("found %d jobs, want %d", l, 0)
	}
}
