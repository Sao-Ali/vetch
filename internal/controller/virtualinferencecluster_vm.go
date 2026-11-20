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

package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

const vmAgentPortName = "agent"

func virtualMachineName(clusterName string) string {
	return childName(clusterName, "-vm")
}

func (r *VirtualInferenceClusterReconciler) reconcileVM(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
	bootstrapUID string,
) (*kubevirtv1.VirtualMachine, error) {
	if r.vmAPIAvailable != nil && !*r.vmAPIAvailable {
		return nil, errors.New("KubeVirt VirtualMachine API is not installed")
	}
	name := virtualMachineName(cluster.Name)
	key := client.ObjectKey{Namespace: cluster.Namespace, Name: name}
	vm := &kubevirtv1.VirtualMachine{}
	if err := r.Get(ctx, key, vm); err != nil {
		if meta.IsNoMatchError(err) {
			return nil, fmt.Errorf("KubeVirt VirtualMachine API is not installed: %w", err)
		}
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("get VM %q: %w", name, err)
		}
		vm = &kubevirtv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: cluster.Namespace}}
		if err := controllerutil.SetControllerReference(cluster, vm, r.Scheme); err != nil {
			return nil, fmt.Errorf("set owner for VM %q: %w", name, err)
		}
		if err := applyVMFields(vm, cluster, bootstrapUID); err != nil {
			return nil, err
		}
		if err := r.Create(ctx, vm); err != nil {
			return nil, fmt.Errorf("create VM %q: %w", name, err)
		}
		return vm, nil
	}
	if !metav1.IsControlledBy(vm, cluster) {
		return nil, fmt.Errorf("VM %q already exists and is not controlled by VirtualInferenceCluster %q", name, cluster.Name)
	}
	original := vm.DeepCopy()
	if err := applyVMFields(vm, cluster, bootstrapUID); err != nil {
		return nil, err
	}
	if equality.Semantic.DeepEqual(original, vm) {
		return vm, nil
	}
	if err := r.Update(ctx, vm); err != nil {
		return nil, fmt.Errorf("update VM %q: %w", name, err)
	}
	return vm, nil
}

func applyVMFields(vm *kubevirtv1.VirtualMachine, cluster *infrastructurev1alpha1.VirtualInferenceCluster, bootstrapUID string) error {
	settings := cluster.Spec.VM
	if settings == nil || settings.GuestImage == nil || settings.CPUCores == nil || settings.Memory == nil ||
		cluster.Spec.WorkersPerVM == nil || cluster.Spec.Model == nil {
		return fmt.Errorf("VM settings are incomplete for VirtualInferenceCluster %q", cluster.Name)
	}
	guestConfig, err := json.Marshal(struct {
		Model        string `json:"model"`
		WorkersPerVM int32  `json:"workersPerVM"`
	}{Model: *cluster.Spec.Model, WorkersPerVM: *cluster.Spec.WorkersPerVM})
	if cluster.Spec.Worker != nil {
		// Keep bootstrap stable across worker changes. The controller applies the
		// current generation over HTTP once the guest is reachable.
		guestConfig = []byte(`{"model":"bootstrap","workersPerVM":1}`)
	}
	if err != nil {
		return fmt.Errorf("encode guest configuration: %w", err)
	}
	userData := fmt.Sprintf("#cloud-config\nwrite_files:\n  - path: /etc/vetch/config.json\n    permissions: '0644'\n    content: |\n      %s\n", guestConfig)

	if vm.Labels == nil {
		vm.Labels = make(map[string]string)
	}
	vm.Labels["app.kubernetes.io/managed-by"] = applicationName
	vm.Labels[componentLabelKey] = vmComponent
	vm.Labels[clusterUIDLabelKey] = string(cluster.UID)
	strategy := kubevirtv1.RunStrategyAlways
	if vm.Spec.RunStrategy == nil || *vm.Spec.RunStrategy != strategy {
		vm.Spec.RunStrategy = &strategy
	}
	if vm.Spec.Template == nil {
		vm.Spec.Template = &kubevirtv1.VirtualMachineInstanceTemplateSpec{}
	}
	if vm.Spec.Template.ObjectMeta.Labels == nil {
		vm.Spec.Template.ObjectMeta.Labels = make(map[string]string)
	}
	vm.Spec.Template.ObjectMeta.Labels[clusterUIDLabelKey] = string(cluster.UID)
	vm.Spec.Template.ObjectMeta.Labels[componentLabelKey] = vmComponent
	cloudInit := &kubevirtv1.CloudInitNoCloudSource{UserData: userData}
	if cluster.Spec.Worker != nil {
		if vm.Spec.Template.ObjectMeta.Annotations == nil {
			vm.Spec.Template.ObjectMeta.Annotations = make(map[string]string)
		}
		vm.Spec.Template.ObjectMeta.Annotations[bootstrapUIDAnnotation] = bootstrapUID
		cloudInit = &kubevirtv1.CloudInitNoCloudSource{UserDataSecretRef: &corev1.LocalObjectReference{Name: agentSecretName(cluster.Name)}}
	} else {
		delete(vm.Spec.Template.ObjectMeta.Annotations, bootstrapUIDAnnotation)
	}
	domain := &vm.Spec.Template.Spec.Domain
	if domain.CPU == nil {
		domain.CPU = &kubevirtv1.CPU{}
	}
	domain.CPU.Cores = uint32(*settings.CPUCores)
	if domain.Memory == nil {
		domain.Memory = &kubevirtv1.Memory{}
	}
	if domain.Memory.Guest == nil || domain.Memory.Guest.Cmp(*settings.Memory) != 0 {
		memory := settings.Memory.DeepCopy()
		domain.Memory.Guest = &memory
	}
	setVMDisk(&domain.Devices.Disks, vmDiskName)
	setVMDisk(&domain.Devices.Disks, vmConfigDiskName)
	setVMPodNetwork(&vm.Spec.Template.Spec)
	setVMVolume(&vm.Spec.Template.Spec.Volumes, kubevirtv1.Volume{
		Name: vmDiskName,
		VolumeSource: kubevirtv1.VolumeSource{
			ContainerDisk: &kubevirtv1.ContainerDiskSource{Image: *settings.GuestImage},
		},
	})
	setVMVolume(&vm.Spec.Template.Spec.Volumes, kubevirtv1.Volume{
		Name: vmConfigDiskName,
		VolumeSource: kubevirtv1.VolumeSource{
			CloudInitNoCloud: cloudInit,
		},
	})
	return nil
}

// Manage the default pod interface while preserving unrelated interfaces and
// fields defaulted by KubeVirt. Masquerade gives the guest a routed return path
// for requests from other pods, including virt-api port forwarding.
func setVMPodNetwork(spec *kubevirtv1.VirtualMachineInstanceSpec) {
	interfaces := &spec.Domain.Devices.Interfaces
	index := len(*interfaces)
	for i := range *interfaces {
		if (*interfaces)[i].Name == kubevirtv1.DefaultPodNetwork().Name {
			index = i
			break
		}
	}
	if index == len(*interfaces) {
		*interfaces = append(*interfaces, kubevirtv1.Interface{Name: kubevirtv1.DefaultPodNetwork().Name})
	}
	iface := &(*interfaces)[index]
	iface.InterfaceBindingMethod = kubevirtv1.InterfaceBindingMethod{Masquerade: &kubevirtv1.InterfaceMasquerade{}}
	iface.Binding = nil
	iface.Ports = []kubevirtv1.Port{{Name: vmAgentPortName, Port: 8080, Protocol: string(corev1.ProtocolTCP)}}
	for i := range spec.Networks {
		if spec.Networks[i].Name == kubevirtv1.DefaultPodNetwork().Name {
			spec.Networks[i].NetworkSource = kubevirtv1.NetworkSource{Pod: &kubevirtv1.PodNetwork{}}
			return
		}
	}
	spec.Networks = append(spec.Networks, *kubevirtv1.DefaultPodNetwork())
}

func setVMDisk(disks *[]kubevirtv1.Disk, name string) {
	for i := range *disks {
		if (*disks)[i].Name == name {
			if (*disks)[i].Disk == nil || (*disks)[i].Disk.Bus != kubevirtv1.DiskBusVirtio {
				(*disks)[i].DiskDevice = kubevirtv1.DiskDevice{Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio}}
			}
			return
		}
	}
	*disks = append(*disks, kubevirtv1.Disk{
		Name: name,
		DiskDevice: kubevirtv1.DiskDevice{
			Disk: &kubevirtv1.DiskTarget{Bus: kubevirtv1.DiskBusVirtio},
		},
	})
}

func setVMVolume(volumes *[]kubevirtv1.Volume, desired kubevirtv1.Volume) {
	for i := range *volumes {
		if (*volumes)[i].Name != desired.Name {
			continue
		}
		actual := &(*volumes)[i]
		if desired.ContainerDisk != nil && actual.ContainerDisk != nil &&
			actual.ContainerDisk.Image == desired.ContainerDisk.Image {
			return
		}
		if desired.CloudInitNoCloud != nil && actual.CloudInitNoCloud != nil &&
			actual.CloudInitNoCloud.UserData == desired.CloudInitNoCloud.UserData {
			return
		}
		actual.VolumeSource = desired.VolumeSource
		return
	}
	*volumes = append(*volumes, desired)
}

func (r *VirtualInferenceClusterReconciler) deleteOwnedVM(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
) (bool, error) {
	if r.vmAPIAvailable != nil && !*r.vmAPIAvailable {
		return false, nil
	}
	name := virtualMachineName(cluster.Name)
	vm := &kubevirtv1.VirtualMachine{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: name}, vm); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("get VM %q: %w", name, err)
	}
	if !metav1.IsControlledBy(vm, cluster) {
		return false, nil
	}
	if vm.DeletionTimestamp == nil {
		if err := r.Delete(ctx, vm); err != nil && !apierrors.IsNotFound(err) {
			return true, fmt.Errorf("delete VM %q: %w", name, err)
		}
	}
	return true, nil
}
