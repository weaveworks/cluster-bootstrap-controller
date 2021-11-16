package test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ptrutils "k8s.io/utils/pointer"

	capiv1alpha1 "github.com/weaveworks/cluster-bootstrap-controller/api/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// MatchLabels is an option func for MakeClusterBootstrapConfig to set the labels on
// the new config.
func MatchLabels(labels map[string]string) func(*capiv1alpha1.ClusterBootstrapConfig) {
	return func(c *capiv1alpha1.ClusterBootstrapConfig) {
		c.Spec.ClusterSelector = metav1.LabelSelector{
			MatchLabels: labels,
		}
	}
}

// MakeClusterBootstrapConfig allocates and returns a new
// ClusterBootstrapConfig value.
func MakeClusterBootstrapConfig(name, namespace string, opts ...func(*capiv1alpha1.ClusterBootstrapConfig)) *capiv1alpha1.ClusterBootstrapConfig {
	bc := &capiv1alpha1.ClusterBootstrapConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: capiv1alpha1.ClusterBootstrapConfigSpec{
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
func Labels(l map[string]string) func(*clusterv1.Cluster) {
	return func(c *clusterv1.Cluster) {
		c.SetLabels(l)
	}
}

// MakeCluster allocates and returns a new CAPI Cluster value.
func MakeCluster(name, namespace string, opts ...func(*clusterv1.Cluster)) *clusterv1.Cluster {
	c := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: clusterv1.ClusterSpec{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}
