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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:validation:XValidation:rule="has(self.nodes) || has(self.vmCount)",message="either nodes or vmCount must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.vmCount) || !has(self.nodes) || self.nodes == 0",message="nodes must be zero or omitted in VM mode"
// +kubebuilder:validation:XValidation:rule="!has(self.vmCount) || self.vmCount == 0 || (has(self.workersPerVM) && has(self.model) && has(self.vm) && has(self.vm.guestImage) && has(self.vm.cpuCores) && has(self.vm.memory))",message="positive vmCount requires workersPerVM, model, guestImage, cpuCores, and memory"
type VirtualInferenceClusterSpec struct {
	// nodes requests legacy dummy ConfigMaps.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Nodes *int32 `json:"nodes,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	// +optional
	VMCount *int32 `json:"vmCount,omitempty"`

	// workersPerVM is a declared count, not observed capacity.
	// +kubebuilder:validation:Minimum=1
	// +optional
	WorkersPerVM *int32 `json:"workersPerVM,omitempty"`

	// model is an opaque identifier shared by the workers.
	// +kubebuilder:validation:MinLength=1
	// +optional
	Model *string `json:"model,omitempty"`

	// +optional
	VM *VirtualInferenceVMTemplate `json:"vm,omitempty"`
}

type VirtualInferenceVMTemplate struct {
	// guestImage is a bootable KubeVirt containerDisk OCI image reference.
	// +kubebuilder:validation:MinLength=1
	// +optional
	GuestImage *string `json:"guestImage,omitempty"`

	// cpuCores specifies guest vCPUs, not dedicated host cores.
	// +kubebuilder:validation:Minimum=1
	// +optional
	CPUCores *int32 `json:"cpuCores,omitempty"`

	// memory specifies guest memory, excluding host overhead.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$`
	// +kubebuilder:validation:XValidation:rule="quantity(self).isGreaterThan(quantity('0'))",message="memory must be positive"
	// +optional
	Memory *resource.Quantity `json:"memory,omitempty"`
}

type VirtualInferenceClusterStatus struct {
	// declaredWorkerCapacity is vmCount * workersPerVM, not serving capacity.
	// +optional
	DeclaredWorkerCapacity *int32 `json:"declaredWorkerCapacity,omitempty"`

	// Available reflects legacy placeholder reconciliation; VM requests report pending.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type VirtualInferenceCluster struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec VirtualInferenceClusterSpec `json:"spec"`

	// +optional
	Status VirtualInferenceClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

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
