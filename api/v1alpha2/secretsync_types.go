package v1alpha2

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretSyncSpec
type SecretSyncSpec struct {
	// ClusterSelector specifies the label selector to match clusters with
	ClusterSelector metav1.LabelSelector `json:"clusterSelector"`
	// SecretRef specifies the Secret to be bootstrapped to the matched clusters
	// Secret must be in the same namespace of the SecretSync object
	SecretRef v1.LocalObjectReference `json:"secretRef"`
	// TargetNamespace specifies the namespace which the secret should be bootstrapped in
	// The default value is the namespace of the referenced secret
	//+optional
	TargetNamespace string `json:"targetNamespace"`
}

// SecretSyncStatus secretsync object status
type SecretSyncStatus struct {
	// SecretVersions a map contains the ResourceVersion of the secret of each cluster
	// Cluster name is the key and secret's ResourceVersion is the value
	SecretVersions map[string]string `json:"versions"`
}

// SetClusterSecretVersion sets the latest secret version of the given cluster
func (s *SecretSyncStatus) SetClusterSecretVersion(cluster, version string) {
	if s.SecretVersions == nil {
		s.SecretVersions = make(map[string]string)
	}
	s.SecretVersions[cluster] = version
}

// GetClusterSecretVersion returns secret's ResourceVersion of the given cluster
func (s *SecretSyncStatus) GetClusterSecretVersion(cluster string) string {
	return s.SecretVersions[cluster]
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:scope:namespaced

type SecretSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SecretSyncSpec   `json:"spec,omitempty"`
	Status            SecretSyncStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type SecretSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SecretSync `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecretSync{}, &SecretSyncList{})
}
