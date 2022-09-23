package test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptrutils "k8s.io/utils/pointer"

	capiv1alpha2 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha2"
	gitopsv1alpha1 "github.com/weaveworks/cluster-controller/api/v1alpha1"
)

// MatchLabels is an option func for MakeClusterBootstrapConfig to set the labels on
// the new config.
func MatchLabels(labels map[string]string) func(*capiv1alpha2.ClusterBootstrapConfig) {
	return func(c *capiv1alpha2.ClusterBootstrapConfig) {
		c.Spec.ClusterSelector = metav1.LabelSelector{
			MatchLabels: labels,
		}
	}
}

// MakeClusterBootstrapConfig allocates and returns a new
// ClusterBootstrapConfig value.
func MakeClusterBootstrapConfig(name, namespace string, opts ...func(*capiv1alpha2.ClusterBootstrapConfig)) *capiv1alpha2.ClusterBootstrapConfig {
	bc := &capiv1alpha2.ClusterBootstrapConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: capiv1alpha2.ClusterBootstrapConfigSpec{
			Template: capiv1alpha2.JobTemplate{
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

// Labels a cluster option that sets the labels on the cluster.
func Labels(l map[string]string) func(*gitopsv1alpha1.GitopsCluster) {
	return func(c *gitopsv1alpha1.GitopsCluster) {
		c.SetLabels(l)
	}
}

// MakeCluster allocates and returns a new CAPI Cluster value.
func MakeCluster(name, namespace string, opts ...func(*gitopsv1alpha1.GitopsCluster)) *gitopsv1alpha1.GitopsCluster {
	c := &gitopsv1alpha1.GitopsCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gitopsv1alpha1.GitopsClusterSpec{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}
