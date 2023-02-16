package controllers

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ptrutils "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
	"github.com/weaveworks/cluster-bootstrap-controller/test"
)

func Test_bootstrapClusterWithConfig(t *testing.T) {
	bc := makeTestClusterBootstrapConfig()
	cl := makeTestCluster()
	tc := makeTestClient(t)

	if err := bootstrapClusterWithConfig(context.TODO(), logr.Discard(), tc, cl, bc); err != nil {
		t.Fatal(err)
	}

	var jobList batchv1.JobList
	if err := tc.List(context.TODO(), &jobList, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}
	want := []batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName:    "setup-something-",
				Namespace:       testNamespace,
				ResourceVersion: "1",
			},
			Spec: batchv1.JobSpec{
				BackoffLimit: ptrutils.Int32Ptr(13),
				Template: corev1.PodTemplateSpec{
					Spec: bc.Spec.Template.Spec,
				},
			},
		},
	}
	if diff := cmp.Diff(want, jobList.Items, cmpopts.IgnoreFields(metav1.ObjectMeta{}, "Name", "OwnerReferences")); diff != "" {
		t.Fatalf("failed to create jobs from template:\n%s", diff)
	}
}

func Test_bootstrapClusterWithConfig_sets_owner(t *testing.T) {
	bc := makeTestClusterBootstrapConfig()
	cl := makeTestCluster()
	tc := makeTestClient(t)

	if err := bootstrapClusterWithConfig(context.TODO(), logr.Discard(), tc, cl, bc); err != nil {
		t.Fatal(err)
	}

	var jobList batchv1.JobList
	if err := tc.List(context.TODO(), &jobList, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}

	want := []metav1.OwnerReference{
		{
			APIVersion: "gitops.weave.works/v1alpha1",
			Kind:       "GitopsCluster",
			Name:       testClusterName,
		},
	}
	if diff := cmp.Diff(want, jobList.Items[0].ObjectMeta.OwnerReferences); diff != "" {
		t.Fatalf("failed to set job owner:\n%s", diff)
	}
}

func Test_bootstrapClusterWithConfig_fail_to_create_job(t *testing.T) {
	// This is a hacky test for making Create fail because of an unregistered
	// type.
	s := runtime.NewScheme()
	test.AssertNoError(t, gitopsv1alpha1.AddToScheme(s))
	tc := fake.NewClientBuilder().WithScheme(s).Build()
	bc := makeTestClusterBootstrapConfig()
	cl := makeTestCluster()

	err := bootstrapClusterWithConfig(context.TODO(), logr.Discard(), tc, cl, bc)
	test.AssertErrorMatch(t, "failed to create job", err)
}

func makeTestPodSpecWithVolumes(volumes ...corev1.Volume) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Name:    "test",
				Image:   "bash:5.1",
				Command: []string{"ls", "/"},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "kubeconfig",
						MountPath: "/etc/wego",
						ReadOnly:  true,
					},
				},
			},
		},
		Volumes:       volumes,
		RestartPolicy: corev1.RestartPolicyOnFailure,
	}
}

func Test_bootstrapClusterWithConfig_sets_job_ttl(t *testing.T) {
	bc := makeTestClusterBootstrapConfig(func(cfg *capiv1alpha1.ClusterBootstrapConfig) {
		cfg.Spec.Template.TTLSecondsAfterFinished = ptrutils.Int32Ptr(66)
	})
	cl := makeTestCluster()
	tc := makeTestClient(t)

	if err := bootstrapClusterWithConfig(context.TODO(), logr.Discard(), tc, cl, bc); err != nil {
		t.Fatal(err)
	}

	var jobList batchv1.JobList
	if err := tc.List(context.TODO(), &jobList, client.InNamespace(testNamespace)); err != nil {
		t.Fatal(err)
	}

	if ttl := *jobList.Items[0].Spec.TTLSecondsAfterFinished; ttl != 66 {
		t.Fatalf("got TTLSecondsAfterFinished %v, want %v", ttl, 66)
	}
}

func makeTestClusterBootstrapConfig(opts ...func(*capiv1alpha1.ClusterBootstrapConfig)) *capiv1alpha1.ClusterBootstrapConfig {
	bc := &capiv1alpha1.ClusterBootstrapConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigName,
			Namespace: testNamespace,
		},
		Spec: capiv1alpha1.ClusterBootstrapConfigSpec{
			ClusterSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"testing": "label",
				},
			},
			Template: capiv1alpha1.JobTemplate{
				GenerateName: "setup-something-",
				BackoffLimit: ptrutils.Int32Ptr(13),
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "test",
							Image:   "bash:5.1",
							Command: []string{"ls", "/"},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "kubeconfig",
									MountPath: "/etc/wego",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						makeTestVolume("kubeconfig", "test-secret"),
					},
					RestartPolicy: corev1.RestartPolicyOnFailure,
				},
			},
		},
	}
	for _, o := range opts {
		o(bc)
	}

	return bc
}
