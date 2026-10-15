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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

const testNamespace = "default"

var _ = Describe("VirtualInferenceCluster Controller", func() {
	var (
		testContext context.Context
		cluster     *infrastructurev1alpha1.VirtualInferenceCluster
		reconciler  *VirtualInferenceClusterReconciler
		request     reconcile.Request
	)

	BeforeEach(func() {
		testContext = context.Background()
		cluster = &infrastructurev1alpha1.VirtualInferenceCluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-cluster-",
				Namespace:    testNamespace,
			},
			Spec: infrastructurev1alpha1.VirtualInferenceClusterSpec{Nodes: ptr(int32(2))},
		}
		Expect(k8sClient.Create(testContext, cluster)).To(Succeed())
		request = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}
		reconciler = &VirtualInferenceClusterReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	})

	AfterEach(func() {
		if cluster == nil || cluster.Name == "" {
			return
		}
		configMaps := ownedConfigMaps(testContext, cluster)
		for i := range configMaps {
			Expect(k8sClient.Delete(testContext, &configMaps[i])).To(Succeed())
		}
		vm := &kubevirtv1.VirtualMachine{}
		if err := k8sClient.Get(testContext, client.ObjectKey{Namespace: cluster.Namespace, Name: virtualMachineName(cluster.Name)}, vm); err == nil {
			if metav1.IsControlledBy(vm, cluster) {
				Expect(k8sClient.Delete(testContext, vm)).To(Succeed())
			}
		} else {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}
		current := &infrastructurev1alpha1.VirtualInferenceCluster{}
		err := k8sClient.Get(testContext, client.ObjectKeyFromObject(cluster), current)
		if err == nil {
			Expect(k8sClient.Delete(testContext, current)).To(Succeed())
			return
		}
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("creates deterministic owned ConfigMaps and reports availability", func() {
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())

		for nodeIndex := range int32(2) {
			configMap := getConfigMap(testContext, cluster.Namespace, dummyNodeName(cluster.Name, nodeIndex))
			Expect(metav1.IsControlledBy(configMap, cluster)).To(BeTrue())
			Expect(configMap.Labels).To(HaveKeyWithValue(componentLabelKey, dummyNodeComponent))
			Expect(configMap.Labels).To(HaveKeyWithValue(clusterUIDLabelKey, string(cluster.UID)))
			Expect(configMap.Data).To(HaveKeyWithValue("cluster", cluster.Name))
		}

		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		condition := meta.FindStatusCondition(current.Status.Conditions, availableConditionType)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("Reconciled"))
		Expect(condition.ObservedGeneration).To(Equal(current.Generation))
	})

	It("does not write children again when the desired state already exists", func() {
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		first := getConfigMap(testContext, cluster.Namespace, dummyNodeName(cluster.Name, 0))

		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		second := getConfigMap(testContext, cluster.Namespace, dummyNodeName(cluster.Name, 0))
		Expect(second.ResourceVersion).To(Equal(first.ResourceVersion))
	})

	It("scales dummy nodes up and down", func() {
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())

		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec.Nodes = ptr(int32(4))
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownedConfigMaps(testContext, current)).To(HaveLen(4))

		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec.Nodes = ptr(int32(1))
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownedConfigMaps(testContext, current)).To(HaveLen(1))
		Expect(getConfigMap(testContext, current.Namespace, dummyNodeName(current.Name, 0))).NotTo(BeNil())
	})

	It("refuses to adopt a ConfigMap with a desired name", func() {
		collision := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dummyNodeName(cluster.Name, 0),
				Namespace: cluster.Namespace,
			},
			Data: map[string]string{"owner": "someone-else"},
		}
		Expect(k8sClient.Create(testContext, collision)).To(Succeed())
		DeferCleanup(func() {
			if err := k8sClient.Delete(testContext, collision); err != nil {
				Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}
		})

		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).To(MatchError(ContainSubstring("is not controlled by")))
		unchanged := getConfigMap(testContext, collision.Namespace, collision.Name)
		Expect(unchanged.Data).To(Equal(map[string]string{"owner": "someone-else"}))
		Expect(unchanged.OwnerReferences).To(BeEmpty())

		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		condition := meta.FindStatusCondition(current.Status.Conditions, availableConditionType)
		Expect(condition).NotTo(BeNil())
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("ReconciliationFailed"))
	})

	It("treats a missing parent as successfully reconciled", func() {
		Expect(k8sClient.Delete(testContext, cluster)).To(Succeed())
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
	})

	It("keeps legacy reconciliation available without the KubeVirt API", func() {
		reconciler.vmAPIAvailable = ptr(false)
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownedConfigMaps(testContext, cluster)).To(HaveLen(2))

		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).To(MatchError(ContainSubstring("KubeVirt VirtualMachine API is not installed")))
		condition := meta.FindStatusCondition(getCluster(testContext, client.ObjectKeyFromObject(cluster)).Status.Conditions, availableConditionType)
		Expect(condition.Reason).To(Equal("ReconciliationFailed"))
	})

	It("creates one configured VM and removes legacy placeholders", func() {
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownedConfigMaps(testContext, cluster)).To(HaveLen(2))

		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())

		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		condition := meta.FindStatusCondition(current.Status.Conditions, availableConditionType)
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal("VMProvisioningPending"))
		Expect(condition.ObservedGeneration).To(Equal(current.Generation))
		Expect(current.Status.DeclaredWorkerCapacity).To(Equal(ptr(int32(2))))
		Expect(ownedConfigMaps(testContext, current)).To(BeEmpty())
		vm := getVM(testContext, current)
		Expect(metav1.IsControlledBy(vm, current)).To(BeTrue())
		Expect(vm.Spec.RunStrategy).To(Equal(ptr(kubevirtv1.RunStrategyAlways)))
		Expect(vm.Spec.Template.Spec.Domain.CPU.Cores).To(Equal(uint32(2)))
		Expect(vm.Spec.Template.Spec.Domain.Memory.Guest.Cmp(resource.MustParse("4Gi"))).To(Equal(0))
		Expect(vm.Spec.Template.Spec.Domain.Devices.Disks).To(HaveLen(2))
		Expect(vm.Spec.Template.Spec.Volumes).To(ContainElement(And(
			HaveField("Name", vmDiskName),
			HaveField("ContainerDisk.Image", *current.Spec.VM.GuestImage),
		)))
		Expect(vm.Spec.Template.Spec.Volumes).To(ContainElement(And(
			HaveField("Name", vmConfigDiskName),
			HaveField("CloudInitNoCloud.UserData", ContainSubstring(`{"model":"example/model-v1","workersPerVM":2}`)),
		)))

		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		again := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		Expect(again.ResourceVersion).To(Equal(current.ResourceVersion))
		Expect(getVM(testContext, current).ResourceVersion).To(Equal(vm.ResourceVersion))
	})

	It("updates the VM when declared settings change", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		first := getVM(testContext, current)

		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec.VM.GuestImage = ptr("example.com/new-guest:v2")
		current.Spec.WorkersPerVM = ptr(int32(3))
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		updated := getVM(testContext, current)
		Expect(updated.UID).To(Equal(first.UID))
		Expect(updated.ResourceVersion).NotTo(Equal(first.ResourceVersion))
		Expect(updated.Spec.Template.Spec.Volumes).To(ContainElement(And(
			HaveField("Name", vmDiskName), HaveField("ContainerDisk.Image", "example.com/new-guest:v2"),
		)))
		Expect(updated.Spec.Template.Spec.Volumes).To(ContainElement(And(
			HaveField("Name", vmConfigDiskName),
			HaveField("CloudInitNoCloud.UserData", ContainSubstring(`"workersPerVM":3`)),
		)))
		Expect(getCluster(testContext, client.ObjectKeyFromObject(cluster)).Status.DeclaredWorkerCapacity).To(Equal(ptr(int32(3))))
	})

	It("reports VM readiness only after the current VM generation is observed", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		vm := getVM(testContext, current)
		vm.Status.Ready = true
		vm.Status.ObservedGeneration = vm.Generation - 1
		Expect(k8sClient.Status().Update(testContext, vm)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		condition := meta.FindStatusCondition(getCluster(testContext, client.ObjectKeyFromObject(cluster)).Status.Conditions, availableConditionType)
		Expect(condition.Reason).To(Equal("VMProvisioningPending"))

		vm = getVM(testContext, current)
		vm.Status.ObservedGeneration = vm.Generation
		Expect(k8sClient.Status().Update(testContext, vm)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		condition = meta.FindStatusCondition(getCluster(testContext, client.ObjectKeyFromObject(cluster)).Status.Conditions, availableConditionType)
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("VMReady"))
	})

	It("refuses to adopt an unrelated VM with the desired name", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		collision := &kubevirtv1.VirtualMachine{ObjectMeta: metav1.ObjectMeta{
			Name: virtualMachineName(cluster.Name), Namespace: cluster.Namespace,
		}, Spec: kubevirtv1.VirtualMachineSpec{RunStrategy: ptr(kubevirtv1.RunStrategyAlways)}}
		Expect(k8sClient.Create(testContext, collision)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(testContext, collision)).To(Succeed()) })
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).To(MatchError(ContainSubstring("is not controlled by")))
		unchanged := getVM(testContext, current)
		Expect(unchanged.OwnerReferences).To(BeEmpty())
		Expect(unchanged.ResourceVersion).To(Equal(collision.ResourceVersion))
		condition := meta.FindStatusCondition(getCluster(testContext, client.ObjectKeyFromObject(cluster)).Status.Conditions, availableConditionType)
		Expect(condition.Reason).To(Equal("ReconciliationFailed"))
	})

	It("deletes its VM when the requested count becomes zero", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())

		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec.VMCount = ptr(int32(0))
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		vm := &kubevirtv1.VirtualMachine{}
		err = k8sClient.Get(testContext, client.ObjectKey{Namespace: cluster.Namespace, Name: virtualMachineName(cluster.Name)}, vm)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		condition := meta.FindStatusCondition(getCluster(testContext, client.ObjectKeyFromObject(cluster)).Status.Conditions, availableConditionType)
		Expect(condition.Reason).To(Equal("NoVMsRequested"))
	})

	It("removes its VM when switching back to legacy nodes", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(getVM(testContext, current)).NotTo(BeNil())

		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = infrastructurev1alpha1.VirtualInferenceClusterSpec{Nodes: ptr(int32(1))}
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		vm := &kubevirtv1.VirtualMachine{}
		err = k8sClient.Get(testContext, client.ObjectKey{Namespace: cluster.Namespace, Name: virtualMachineName(cluster.Name)}, vm)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		Expect(ownedConfigMaps(testContext, current)).To(HaveLen(1))
	})

	It("rejects invalid VM API shapes", func() {
		cases := []struct {
			name   string
			mutate func(*infrastructurev1alpha1.VirtualInferenceClusterSpec)
		}{
			{"no mode", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) {
				*s = infrastructurev1alpha1.VirtualInferenceClusterSpec{}
			}},
			{"negative nodes", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) {
				*s = infrastructurev1alpha1.VirtualInferenceClusterSpec{Nodes: ptr(int32(-1))}
			}},
			{"too many VMs", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.VMCount = ptr(int32(2)) }},
			{"positive nodes in VM mode", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.Nodes = ptr(int32(1)) }},
			{"missing worker count", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.WorkersPerVM = nil }},
			{"missing model", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.Model = nil }},
			{"empty model", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.Model = ptr("") }},
			{"missing image", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.VM.GuestImage = nil }},
			{"zero CPU", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.VM.CPUCores = ptr(int32(0)) }},
			{"zero workers", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) { s.WorkersPerVM = ptr(int32(0)) }},
			{"zero memory", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) {
				s.VM.Memory = ptr(resource.MustParse("0"))
			}},
			{"negative memory at zero VMs", func(s *infrastructurev1alpha1.VirtualInferenceClusterSpec) {
				s.VMCount = ptr(int32(0))
				s.VM.Memory = ptr(resource.MustParse("-1Gi"))
			}},
		}
		for _, tc := range cases {
			By(tc.name)
			spec := validVMSpec()
			tc.mutate(&spec)
			candidate := &infrastructurev1alpha1.VirtualInferenceCluster{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "invalid-vm-", Namespace: testNamespace},
				Spec:       spec,
			}
			Expect(k8sClient.Create(testContext, candidate)).NotTo(Succeed())
		}
	})

	It("accepts zero VMs and retained settings", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = validVMSpec()
		current.Spec.VMCount = ptr(int32(0))
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err := reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		Expect(current.Status.DeclaredWorkerCapacity).To(Equal(ptr(int32(0))))
		condition := meta.FindStatusCondition(current.Status.Conditions, availableConditionType)
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal("NoVMsRequested"))
		Expect(condition.ObservedGeneration).To(Equal(current.Generation))
	})

	It("accepts zero VMs without a template", func() {
		current := getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec = infrastructurev1alpha1.VirtualInferenceClusterSpec{VMCount: ptr(int32(0))}
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
	})

	It("rejects malformed memory quantities", func() {
		candidate := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "infrastructure.vetch.io/v1alpha1",
			"kind":       "VirtualInferenceCluster",
			"metadata":   map[string]any{"generateName": "invalid-memory-", "namespace": testNamespace},
			"spec": map[string]any{
				"vmCount": int64(1), "workersPerVM": int64(2), "model": "example/model-v1",
				"vm": map[string]any{"guestImage": "example/image:latest", "cpuCores": int64(2), "memory": "bogus"},
			},
		}}
		Expect(k8sClient.Create(testContext, candidate)).NotTo(Succeed())
	})
})

func validVMSpec() infrastructurev1alpha1.VirtualInferenceClusterSpec {
	return infrastructurev1alpha1.VirtualInferenceClusterSpec{
		VMCount: ptr(int32(1)), WorkersPerVM: ptr(int32(2)), Model: ptr("example/model-v1"),
		VM: &infrastructurev1alpha1.VirtualInferenceVMTemplate{
			GuestImage: ptr("registry.example.com/vetch/bootable-guest@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			CPUCores:   ptr(int32(2)), Memory: ptr(resource.MustParse("4Gi")),
		},
	}
}

func ptr[T any](value T) *T { return &value }

func getCluster(ctx context.Context, key types.NamespacedName) *infrastructurev1alpha1.VirtualInferenceCluster {
	cluster := &infrastructurev1alpha1.VirtualInferenceCluster{}
	ExpectWithOffset(1, k8sClient.Get(ctx, key, cluster)).To(Succeed())
	return cluster
}

func getConfigMap(ctx context.Context, namespace string, name string) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{}
	ExpectWithOffset(1, k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, configMap)).To(Succeed())
	return configMap
}

func getVM(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster) *kubevirtv1.VirtualMachine {
	vm := &kubevirtv1.VirtualMachine{}
	ExpectWithOffset(1, k8sClient.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace, Name: virtualMachineName(cluster.Name),
	}, vm)).To(Succeed())
	return vm
}

func ownedConfigMaps(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
) []corev1.ConfigMap {
	configMaps := &corev1.ConfigMapList{}
	ExpectWithOffset(1, k8sClient.List(ctx, configMaps, client.InNamespace(cluster.Namespace))).To(Succeed())
	owned := make([]corev1.ConfigMap, 0, len(configMaps.Items))
	for i := range configMaps.Items {
		if metav1.IsControlledBy(&configMaps.Items[i], cluster) {
			owned = append(owned, configMaps.Items[i])
		}
	}
	return owned
}
