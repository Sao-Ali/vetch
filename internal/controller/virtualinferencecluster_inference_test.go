package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
	"github.com/Sao-Ali/vetch/internal/workload"
)

type agentTransport func(*http.Request) (*http.Response, error)

func (transport agentTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

var _ = Describe("Managed inference", func() {
	var cluster *infrastructurev1alpha1.VirtualInferenceCluster
	var reconciler *VirtualInferenceClusterReconciler
	var request reconcile.Request
	var handler http.HandlerFunc
	BeforeEach(func() {
		spec := validVMSpec()
		spec.WorkersPerVM = ptr(int32(1))
		spec.Worker = &infrastructurev1alpha1.InferenceWorkerTemplate{ModelFile: "model.gguf", ModelSHA256: strings.Repeat("a", 64), CPUMillis: 1000, MemoryBytes: 2 << 30, ContextTokens: 2048}
		cluster = &infrastructurev1alpha1.VirtualInferenceCluster{ObjectMeta: metav1.ObjectMeta{GenerateName: "inference-", Namespace: testNamespace}, Spec: spec}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		request = reconcile.Request{NamespacedName: client.ObjectKeyFromObject(cluster)}
		handler = func(writer http.ResponseWriter, request *http.Request) {
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: agentSecretName(cluster.Name), Namespace: cluster.Namespace}, secret)).To(Succeed())
			Expect(request.Header.Get("Authorization")).To(Equal("Bearer " + string(secret.Data[configTokenKey])))
			var config workload.Config
			Expect(json.NewDecoder(request.Body).Decode(&config)).To(Succeed())
			workload.Write(writer, http.StatusAccepted, workload.Status{DesiredGeneration: config.Generation, AppliedGeneration: config.Generation, Model: config.Model, Workers: []workload.Worker{{ID: "worker-0", State: workload.Ready}}})
		}
		reconciler = &VirtualInferenceClusterReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), AgentClient: &http.Client{Transport: agentTransport(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler(recorder, request)
			return recorder.Result(), nil
		})}}
	})
	AfterEach(func() {
		Expect(reconciler.deleteOwnedAgentAccess(ctx, cluster)).To(Succeed())
		Expect(reconciler.deleteOwnedAgentService(ctx, cluster)).To(Succeed())
		_, err := reconciler.deleteOwnedVM(ctx, cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
	})
	markVMReady := func() {
		vm := getVM(context.Background(), cluster)
		vm.Status.Ready = true
		vm.Status.ObservedGeneration = vm.Generation
		Expect(k8sClient.Status().Update(ctx, vm)).To(Succeed())
	}
	It("keeps credentials private and stable and restricts agent ingress", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		secret := &corev1.Secret{}
		key := client.ObjectKey{Name: agentSecretName(cluster.Name), Namespace: cluster.Namespace}
		Expect(k8sClient.Get(ctx, key, secret)).To(Succeed())
		Expect(metav1.IsControlledBy(secret, cluster)).To(BeTrue())
		Expect(secret.Immutable != nil && *secret.Immutable).To(BeTrue())
		Expect(secret.Data[configTokenKey]).To(HaveLen(64))
		vm := getVM(ctx, cluster)
		cloudInit := vm.Spec.Template.Spec.Volumes[1].CloudInitNoCloud
		Expect(cloudInit.UserData).To(BeEmpty())
		Expect(cloudInit.UserDataSecretRef.Name).To(Equal(key.Name))
		encoded, err := json.Marshal(vm)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Contains(string(encoded), string(secret.Data[configTokenKey]))).To(BeFalse())
		policy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace}, policy)).To(Succeed())
		Expect(metav1.IsControlledBy(policy, cluster)).To(BeTrue())
		Expect(policy.Spec.PodSelector.MatchLabels[clusterUIDLabelKey]).To(Equal(string(cluster.UID)))
		Expect(policy.Spec.Ingress).To(HaveLen(1))
		peers := policy.Spec.Ingress[0].From
		Expect(peers).To(HaveLen(2))
		Expect(peers[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]).To(Equal(defaultControllerNamespace))
		Expect(peers[0].PodSelector.MatchLabels["control-plane"]).To(Equal("controller-manager"))
		Expect(peers[1].NamespaceSelector).To(BeNil())
		Expect(peers[1].PodSelector.MatchLabels["app"]).To(Equal("vetch-gateway"))
		originalSecretVersion, originalPolicyVersion := secret.ResourceVersion, policy.ResourceVersion
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, secret)).To(Succeed())
		Expect(secret.ResourceVersion).To(Equal(originalSecretVersion))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), policy)).To(Succeed())
		Expect(policy.ResourceVersion).To(Equal(originalPolicyVersion))
		reconciler.ControllerNamespace = "custom-control-plane"
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(policy), policy)).To(Succeed())
		Expect(policy.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]).To(Equal("custom-control-plane"))
	})
	It("requires a new VM generation when the bootstrap credential is replaced", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		vm := getVM(ctx, cluster)
		oldUID := vm.Spec.Template.ObjectMeta.Annotations[bootstrapUIDAnnotation]
		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: agentSecretName(cluster.Name), Namespace: cluster.Namespace}, secret)).To(Succeed())
		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		updated := getVM(ctx, cluster)
		Expect(updated.Generation).To(BeNumerically(">", vm.Generation))
		Expect(updated.Spec.Template.ObjectMeta.Annotations[bootstrapUIDAnnotation]).ToNot(Equal(oldUID))
	})
	It("does not adopt or overwrite unrelated credentials or policies", func() {
		objects := []client.Object{
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: agentSecretName(cluster.Name), Namespace: cluster.Namespace}},
			&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace},
				Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}}},
		}
		for _, object := range objects {
			Expect(k8sClient.Create(ctx, object)).To(Succeed())
			version := object.GetResourceVersion()
			_, err := reconciler.Reconcile(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed())
			Expect(object.GetResourceVersion()).To(Equal(version))
			Expect(k8sClient.Delete(ctx, object)).To(Succeed())
			Expect(reconciler.deleteOwnedAgentAccess(ctx, cluster)).To(Succeed())
		}
	})
	It("cleans up access resources when VM count reaches zero", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		current := getCluster(ctx, request.NamespacedName)
		current.Spec.VMCount = ptr(int32(0))
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		for range 2 {
			_, err = reconciler.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: agentSecretName(cluster.Name), Namespace: cluster.Namespace}, &corev1.Secret{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace}, &networkingv1.NetworkPolicy{}))).To(BeTrue())
	})
	It("requires current serving readiness and applies worker changes without changing VM generation", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		current := getCluster(ctx, request.NamespacedName)
		Expect(meta.FindStatusCondition(current.Status.Conditions, "Available").Status).To(Equal(metav1.ConditionFalse))
		markVMReady()
		result, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(AgentPollInterval))
		current = getCluster(ctx, request.NamespacedName)
		Expect(meta.FindStatusCondition(current.Status.Conditions, "Available").Reason).To(Equal("InferenceReady"))
		Expect(current.Status.ReadyWorkers).To(Equal(int32(1)))
		Expect(current.Status.AppliedGeneration).To(Equal(current.Generation))
		vm := getVM(ctx, current)
		service := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: AgentServiceName(cluster.Name)}, service)).To(Succeed())
		Expect(metav1.IsControlledBy(service, current)).To(BeTrue())
		Expect(service.Spec.Selector[clusterUIDLabelKey]).To(Equal(string(current.UID)))
		Expect(vm.Spec.Template.ObjectMeta.Labels[clusterUIDLabelKey]).To(Equal(string(current.UID)))
		conditionBefore := meta.FindStatusCondition(current.Status.Conditions, "WorkersReady").DeepCopy()
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		current = getCluster(ctx, request.NamespacedName)
		Expect(meta.FindStatusCondition(current.Status.Conditions, "WorkersReady").LastTransitionTime).To(Equal(conditionBefore.LastTransitionTime))
		current.Spec.Worker.ContextTokens = 1024
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(getVM(ctx, current).Generation).To(Equal(vm.Generation))
	})
	It("withdraws serving capacity when agent rejects configuration", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		markVMReady()
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		handler = func(writer http.ResponseWriter, _ *http.Request) {
			workload.Fail(writer, 422, "invalid_config", "invalid")
		}
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		current := getCluster(ctx, request.NamespacedName)
		Expect(current.Status.ReadyWorkers).To(BeZero())
		Expect(meta.FindStatusCondition(current.Status.Conditions, "VMReady").Status).To(Equal(metav1.ConditionTrue))
		Expect(meta.FindStatusCondition(current.Status.Conditions, "Available").Reason).To(Equal("ConfigRejected"))
		Expect(meta.FindStatusCondition(current.Status.Conditions, "WorkersReady").Status).To(Equal(metav1.ConditionFalse))
	})
	It("reports failures and degradation even when a stale ready flag remains set", func() {
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		markVMReady()
		for _, test := range []struct {
			kind   kubevirtv1.VirtualMachineConditionType
			reason string
		}{
			{kubevirtv1.VirtualMachineFailure, vmFailed}, {kubevirtv1.VirtualMachinePaused, "VMDegraded"},
		} {
			vm := getVM(ctx, cluster)
			vm.Status.Conditions = []kubevirtv1.VirtualMachineCondition{{Type: test.kind, Status: corev1.ConditionTrue, Reason: "TestFailure", Message: "test observation"}}
			Expect(k8sClient.Status().Update(ctx, vm)).To(Succeed())
			_, err = reconciler.Reconcile(ctx, request)
			Expect(err).NotTo(HaveOccurred())
			current := getCluster(ctx, request.NamespacedName)
			Expect(meta.FindStatusCondition(current.Status.Conditions, "Available").Reason).To(Equal(test.reason))
		}
	})
	It("does not adopt an unrelated agent Service", func() {
		service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace}, Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 9000}}}}
		Expect(k8sClient.Create(ctx, service)).To(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, service)).To(Succeed()) }()
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).To(HaveOccurred())
		unchanged := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(service), unchanged)).To(Succeed())
		Expect(unchanged.ResourceVersion).To(Equal(service.ResourceVersion))
	})
})

func TestAgentServiceNameIsValidForEveryResourceName(t *testing.T) {
	for _, name := range []string{"demo", "9numeric", "cluster.with.dots", strings.Repeat("a", 253)} {
		serviceName := AgentServiceName(name)
		if errors := validation.IsDNS1035Label(serviceName); len(errors) != 0 {
			t.Fatalf("Service name for %q is invalid: %v", name, errors)
		}
		if AgentServiceName(name) != serviceName {
			t.Fatal("Service naming is not deterministic")
		}
	}
	if AgentServiceName("cluster.with.dots") == AgentServiceName("cluster-with-dots") {
		t.Fatal("normalization caused a Service name collision")
	}
}
