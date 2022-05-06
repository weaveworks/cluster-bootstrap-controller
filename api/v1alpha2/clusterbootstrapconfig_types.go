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

package v1alpha2

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultWaitDuration = time.Second * 60

const BootstrappedAnnotation = "capi.weave.works/bootstrapped"

// JobTemplate describes a job to create
type JobTemplate struct {
	// DNS1032 compatible name used as a prefix when generating the Job from the
	// Spec.
	GenerateName string `json:"generateName"`
	// Specifies the number of retries before marking this job failed.
	// Defaults to 6
	//+kubebuilder:validation:Optional
	//+kubebuilder:default:=6
	BackoffLimit *int32 `json:"backoffLimit,optional"`
	// A batch/v1 Job is created with the Spec as the PodSpec.
	Spec corev1.PodSpec `json:"spec"`
}

// ClusterBootstrapConfigSpec defines the desired state of ClusterBootstrapConfig
type ClusterBootstrapConfigSpec struct {
	ClusterSelector metav1.LabelSelector `json:"clusterSelector"`
	Template        JobTemplate          `json:"jobTemplate"`

	// Wait for the remote cluster to be "ready" before creating the jobs.
	// Defaults to false.
	//+kubebuilder:default:false
	//+optional
	RequireClusterReady bool `json:"requireClusterReady"`
	// When checking for readiness, this is the time to wait before
	// checking again.
	//+kubebuilder:default:60s
	//+optional
	ClusterReadinessBackoff *metav1.Duration `json:"clusterReadinessBackoff,omitempty"`
}

// ClusterBootstrapConfigStatus defines the observed state of ClusterBootstrapConfig
type ClusterBootstrapConfigStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClusterBootstrapConfig is the Schema for the clusterbootstrapconfigs API
type ClusterBootstrapConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterBootstrapConfigSpec   `json:"spec,omitempty"`
	Status ClusterBootstrapConfigStatus `json:"status,omitempty"`
}

// ClusterReadinessRequeue returns the configured ClusterReadinessBackoff or a default
// value if not configured.
func (c ClusterBootstrapConfig) ClusterReadinessRequeue() time.Duration {
	if v := c.Spec.ClusterReadinessBackoff; v != nil {
		return v.Duration
	}
	return defaultWaitDuration
}

//+kubebuilder:object:root=true

// ClusterBootstrapConfigList contains a list of ClusterBootstrapConfig
type ClusterBootstrapConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterBootstrapConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterBootstrapConfig{}, &ClusterBootstrapConfigList{})
}
