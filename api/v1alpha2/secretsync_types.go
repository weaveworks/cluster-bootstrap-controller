package v1alpha2

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SecretSyncSpec struct {
	ClusterSelector metav1.LabelSelector    `json:"clusterSelector"`
	SecretRef       v1.LocalObjectReference `json:"secretRef"`
	//+optional
	TargetNamespace string `json:"targetNamespace"`
}

type SecretSyncStatus struct {
	SecretVersions map[string]string `json:"versions"`
}

// SetClusterSecretVersion set secret's ResourceVersion
func (s *SecretSyncStatus) SetClusterSecretVersion(cluster, secret, version string) {
	if s.SecretVersions == nil {
		s.SecretVersions = make(map[string]string)
	}
	key := fmt.Sprintf("%s/%s", cluster, secret)
	s.SecretVersions[key] = version
}

// GetClusterSecretVersion get secret's ResourceVersion
func (s *SecretSyncStatus) GetClusterSecretVersion(cluster, secret string) string {
	key := fmt.Sprintf("%s/%s", cluster, secret)
	return s.SecretVersions[key]
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
