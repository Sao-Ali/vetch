/*
Copyright 2026.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// VirtualInferenceClusterSpec defines the desired state of VirtualInferenceCluster
type VirtualInferenceClusterSpec struct {
	// nodes is the desired number of virtual inference nodes. Zero requests an empty cluster.
	// +kubebuilder:validation:Minimum=0
	// +required
	Nodes int32 `json:"nodes"`
}

// VirtualInferenceClusterStatus defines the observed state of VirtualInferenceCluster.
type VirtualInferenceClusterStatus struct {
	// conditions represent the current state of the VirtualInferenceCluster resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VirtualInferenceCluster is the Schema for the virtualinferenceclusters API
type VirtualInferenceCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of VirtualInferenceCluster
	// +required
	Spec VirtualInferenceClusterSpec `json:"spec"`

	// status defines the observed state of VirtualInferenceCluster
	// +optional
	Status VirtualInferenceClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// VirtualInferenceClusterList contains a list of VirtualInferenceCluster
type VirtualInferenceClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []VirtualInferenceCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &VirtualInferenceCluster{}, &VirtualInferenceClusterList{})
		return nil
	})
}
