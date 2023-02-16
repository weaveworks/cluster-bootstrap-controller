package controllers

import (
	"testing"

	clustersv1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-bootstrap-controller/test"
)

const (
	testConfigName  = "test-config"
	testClusterName = "test-cluster"
	testNamespace   = "testing"
)

func makeTestCluster(opts ...func(*clustersv1.GitopsCluster)) *clustersv1.GitopsCluster {
	c := &clustersv1.GitopsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testClusterName,
			Namespace: testNamespace,
		},
		Spec: clustersv1.GitopsClusterSpec{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func makeReadyCondition() metav1.Condition {
	return metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionTrue,
	}
}

func makeClusterProvisionedCondition() metav1.Condition {
	return metav1.Condition{
		Type:   clustersv1.ClusterProvisionedCondition,
		Status: metav1.ConditionTrue,
	}
}

func makeNotReadyCondition() metav1.Condition {
	return metav1.Condition{
		Type:   "Ready",
		Status: metav1.ConditionFalse,
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

func makeTestClient(t *testing.T, objs ...runtime.Object) client.Client {
	_, client := makeTestClientAndScheme(t, objs...)
	return client
}

func makeTestClientAndScheme(t *testing.T, objs ...runtime.Object) (*runtime.Scheme, client.Client) {
	t.Helper()
	s := runtime.NewScheme()
	test.AssertNoError(t, clientgoscheme.AddToScheme(s))
	test.AssertNoError(t, capiv1alpha1.AddToScheme(s))
	test.AssertNoError(t, batchv1.AddToScheme(s))
	test.AssertNoError(t, clustersv1.AddToScheme(s))
	return s, fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()
}

func makeTestVolume(name, secretName string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}
