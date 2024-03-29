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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-bootstrap-controller/test"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
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

	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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
	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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
		Type: "Ready", Status: "True",
		LastHeartbeatTime:  metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             "KubeletReady",
		Message:            "kubelet is posting ready status"})

	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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

	// reload the Cluster to check the state
	if err := reconciler.Get(context.TODO(), client.ObjectKeyFromObject(cl), cl); err != nil {
		t.Fatal(err)
	}

	if v := cl.ObjectMeta.Annotations[capiv1alpha1.BootstrapConfigsAnnotation]; v != "testing/test-config" {
		t.Fatalf("got bootstrapped configs %q, want %q", v, "testing/test-config")
	}
}

func TestReconcile_when_cluster_ready_bootstrapped_with_same_config(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{
		Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"})

	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
		c.ObjectMeta.Annotations = map[string]string{
			capiv1alpha1.BootstrappedAnnotation:     "true",
			capiv1alpha1.BootstrapConfigsAnnotation: fmt.Sprintf("%s/%s", bc.Namespace, bc.Name),
		}
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
	if l := len(jobs.Items); l != 0 {
		t.Fatalf("found %d jobs, want %d", l, 0)
	}
}

func TestReconcile_when_cluster_ready_bootstrapped_with_different_config(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{
		Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"})

	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.ObjectMeta.Annotations = map[string]string{
			capiv1alpha1.BootstrapConfigsAnnotation: "unknown/unknown",
		}
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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

func TestReconcile_when_cluster_provisioned(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterProvisioned = true
	})
	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeNotReadyCondition(), makeClusterProvisionedCondition())
	})
	secret := makeTestSecret(types.NamespacedName{
		Name:      cl.GetName() + "-kubeconfig",
		Namespace: cl.GetNamespace(),
	}, map[string][]byte{"value": []byte("testing")})
	// This cheats by using the local client as the remote client to simplify
	// getting the value from the remote client.
	reconciler := makeTestReconciler(t, bc, cl, secret)
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

func TestReconcile_when_cluster_not_provisioned(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterProvisioned = true
	})
	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeNotReadyCondition())
	})
	secret := makeTestSecret(types.NamespacedName{
		Name:      cl.GetName() + "-kubeconfig",
		Namespace: cl.GetNamespace(),
	}, map[string][]byte{"value": []byte("testing")})
	// This cheats by using the local client as the remote client to simplify
	// getting the value from the remote client.
	reconciler := makeTestReconciler(t, bc, cl, secret)
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
	if l := len(jobs.Items); l != 0 {
		t.Fatalf("found %d jobs, want %d", l, 1)
	}
}

func TestReconcile_when_cluster_no_matching_labels(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = map[string]string{
			"will-not-match": "",
		}
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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

func TestReconcile_when_cluster_ready_bootstrapped_with_multiple_config(t *testing.T) {
	// Multiple configs can bootstrap the same cluster
	// If the reconciled cluster is in that list (anywhere) then we don't create
	// jobs.
	bc := makeTestClusterBootstrapConfig(func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.RequireClusterReady = true
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{
		Type: "Ready", Status: "True", LastHeartbeatTime: metav1.Now(), LastTransitionTime: metav1.Now(), Reason: "KubeletReady", Message: "kubelet is posting ready status"})

	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.ObjectMeta.Annotations = map[string]string{
			capiv1alpha1.BootstrappedAnnotation:     "true",
			capiv1alpha1.BootstrapConfigsAnnotation: fmt.Sprintf("%s,%s/%s", "unknown/unknown", bc.GetNamespace(), bc.GetName()),
		}
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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
	if l := len(jobs.Items); l != 0 {
		t.Fatalf("found %d jobs, want %d", l, 0)
	}
}

func TestReconcile_when_cluster_ready_bootstrapped_with_bootstrapped_annotation(t *testing.T) {
	// If the old annotation exists, don't rebootstrap even with newer configs.
	bc := makeTestClusterBootstrapConfig()
	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.ObjectMeta.Annotations = map[string]string{
			capiv1alpha1.BootstrappedAnnotation: "true",
		}
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
	})
	readyNode := makeNode(map[string]string{
		"node-role.kubernetes.io/control-plane": "",
	}, corev1.NodeCondition{
		Type: "Ready", Status: "True",
		LastHeartbeatTime:  metav1.Now(),
		LastTransitionTime: metav1.Now(),
		Reason:             "KubeletReady",
		Message:            "kubelet is posting ready status"})
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
	if l := len(jobs.Items); l != 0 {
		t.Fatalf("found %d jobs, want %d", l, 0)
	}
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
	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = map[string]string{
			"will-not-match": "",
		}
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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

	cl := makeTestCluster(func(c *gitopsv1alpha1.GitopsCluster) {
		c.ObjectMeta.Labels = bc.Spec.ClusterSelector.MatchLabels
		c.Status.Conditions = append(c.Status.Conditions, makeReadyCondition())
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
		w.Header().Set("Content-Type", "application/json")
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
	test.AssertErrorMatch(t, "failed to create RESTMapper from config: Unknown discovery response content-type: text/plain", err)
}

func Test_kubeConfigBytesToClient_with_invalidkubeconfig(t *testing.T) {
	_, err := kubeConfigBytesToClient([]byte("testing"))
	if err == nil {
		t.Fatal("expected to get an error parsing an invalid kubeconfig secret")
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
