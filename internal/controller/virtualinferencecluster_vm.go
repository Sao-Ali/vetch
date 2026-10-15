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

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

func virtualMachineName(clusterName string) string {
	return childName(clusterName, "-vm")
}

func (r *VirtualInferenceClusterReconciler) reconcileVM(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
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
		if err := applyVMFields(vm, cluster); err != nil {
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
	if err := applyVMFields(vm, cluster); err != nil {
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

func applyVMFields(vm *kubevirtv1.VirtualMachine, cluster *infrastructurev1alpha1.VirtualInferenceCluster) error {
	settings := cluster.Spec.VM
	if settings == nil || settings.GuestImage == nil || settings.CPUCores == nil || settings.Memory == nil ||
		cluster.Spec.WorkersPerVM == nil || cluster.Spec.Model == nil {
		return fmt.Errorf("VM settings are incomplete for VirtualInferenceCluster %q", cluster.Name)
	}
	guestConfig, err := json.Marshal(struct {
		Model        string `json:"model"`
		WorkersPerVM int32  `json:"workersPerVM"`
	}{Model: *cluster.Spec.Model, WorkersPerVM: *cluster.Spec.WorkersPerVM})
	if err != nil {
		return fmt.Errorf("encode guest configuration: %w", err)
	}
	userData := fmt.Sprintf("#cloud-config\nwrite_files:\n  - path: /etc/vetch/config.json\n    permissions: '0644'\n    content: |\n      %s\n", guestConfig)

	if vm.Labels == nil {
		vm.Labels = make(map[string]string)
	}
	vm.Labels["app.kubernetes.io/managed-by"] = "vetch"
	vm.Labels[componentLabelKey] = vmComponent
	vm.Labels[clusterUIDLabelKey] = string(cluster.UID)
	strategy := kubevirtv1.RunStrategyAlways
	if vm.Spec.RunStrategy == nil || *vm.Spec.RunStrategy != strategy {
		vm.Spec.RunStrategy = &strategy
	}
	if vm.Spec.Template == nil {
		vm.Spec.Template = &kubevirtv1.VirtualMachineInstanceTemplateSpec{}
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
	setVMVolume(&vm.Spec.Template.Spec.Volumes, kubevirtv1.Volume{
		Name: vmDiskName,
		VolumeSource: kubevirtv1.VolumeSource{
			ContainerDisk: &kubevirtv1.ContainerDiskSource{Image: *settings.GuestImage},
		},
	})
	setVMVolume(&vm.Spec.Template.Spec.Volumes, kubevirtv1.Volume{
		Name: vmConfigDiskName,
		VolumeSource: kubevirtv1.VolumeSource{
			CloudInitNoCloud: &kubevirtv1.CloudInitNoCloudSource{UserData: userData},
		},
	})
	return nil
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
