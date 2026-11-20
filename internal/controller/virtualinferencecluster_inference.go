package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
	"github.com/Sao-Ali/vetch/internal/workload"
)

const AgentPollInterval = 10 * time.Second

const vmFailed = "VMFailed"

func agentConfig(cluster *infrastructurev1alpha1.VirtualInferenceCluster) workload.Config {
	config := workload.Config{Generation: cluster.Generation, Model: *cluster.Spec.Model, WorkersPerVM: *cluster.Spec.WorkersPerVM}
	if worker := cluster.Spec.Worker; worker != nil {
		config.Worker = &workload.WorkerConfig{ModelFile: worker.ModelFile, ModelSHA256: worker.ModelSHA256,
			CPUMillis: worker.CPUMillis, MemoryBytes: worker.MemoryBytes, ContextTokens: worker.ContextTokens}
	}
	return config
}

func AgentServiceName(clusterName string) string {
	name := clusterName + "-agent"
	if len(validation.IsDNS1035Label(name)) == 0 {
		return name
	}
	hash := sha256.Sum256([]byte(clusterName))
	return fmt.Sprintf("vetch-%x-agent", hash[:8])
}

func AgentURL(clusterName, namespace string) string {
	return fmt.Sprintf("http://%s.%s.svc:8080", AgentServiceName(clusterName), namespace)
}

func (r *VirtualInferenceClusterReconciler) reconcileAgentService(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster) error {
	service := &corev1.Service{}
	key := client.ObjectKey{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace}
	err := r.Get(ctx, key, service)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	create := apierrors.IsNotFound(err)
	if create {
		service.ObjectMeta = metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}
		if err := controllerutil.SetControllerReference(cluster, service, r.Scheme); err != nil {
			return err
		}
	} else if !metav1.IsControlledBy(service, cluster) {
		return fmt.Errorf("service %q is not controlled by VirtualInferenceCluster %q", key.Name, cluster.Name)
	}
	original := service.DeepCopy()
	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.Selector = map[string]string{clusterUIDLabelKey: string(cluster.UID), componentLabelKey: vmComponent}
	service.Spec.Ports = []corev1.ServicePort{{Name: vmAgentPortName, Port: 8080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(8080)}}
	if create {
		return r.Create(ctx, service)
	}
	if equality.Semantic.DeepEqual(original, service) {
		return nil
	}
	return r.Update(ctx, service)
}

func (r *VirtualInferenceClusterReconciler) deleteOwnedAgentService(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster) error {
	service := &corev1.Service{}
	if err := r.Get(ctx, client.ObjectKey{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace}, service); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(service, cluster) {
		return nil
	}
	return client.IgnoreNotFound(r.Delete(ctx, service))
}

func vmObservation(vm *kubevirtv1.VirtualMachine) (metav1.ConditionStatus, string, string) {
	if !vm.DeletionTimestamp.IsZero() {
		return metav1.ConditionFalse, vmDeletionPending, "Waiting for VM deletion"
	}
	degradedMessage := ""
	for _, condition := range vm.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case kubevirtv1.VirtualMachineFailure, kubevirtv1.VirtualMachineManualRecoveryRequired:
			return metav1.ConditionFalse, vmFailed, fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
		case kubevirtv1.VirtualMachinePaused, kubevirtv1.VirtualMachineRestartRequired:
			degradedMessage = fmt.Sprintf("%s: %s", condition.Reason, condition.Message)
		}
	}
	switch vm.Status.PrintableStatus {
	case kubevirtv1.VirtualMachineStatusErrImagePull, kubevirtv1.VirtualMachineStatusImagePullBackOff,
		kubevirtv1.VirtualMachineStatusCrashLoopBackOff, kubevirtv1.VirtualMachineStatusUnschedulable,
		kubevirtv1.VirtualMachineStatusPvcNotFound, kubevirtv1.VirtualMachineStatusDataVolumeError:
		return metav1.ConditionFalse, vmFailed, fmt.Sprintf("KubeVirt reports %s", vm.Status.PrintableStatus)
	}
	if degradedMessage != "" {
		return metav1.ConditionFalse, "VMDegraded", degradedMessage
	}
	if vm.Status.Ready && vm.Status.ObservedGeneration >= vm.Generation {
		return metav1.ConditionTrue, "VMReady", "VM is running and ready; inference readiness is reported separately"
	}
	return metav1.ConditionFalse, "VMProvisioningPending", fmt.Sprintf("Waiting for current VM generation: %s", vm.Status.PrintableStatus)
}

func condition(cluster *infrastructurev1alpha1.VirtualInferenceCluster, kind string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type: kind, Status: status, ObservedGeneration: cluster.Generation, Reason: reason, Message: message,
	})
}

func (r *VirtualInferenceClusterReconciler) observeAgent(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster, configToken string) (metav1.ConditionStatus, string, string) {
	cluster.Status.ReadyWorkers = 0
	cluster.Status.AppliedGeneration = 0
	for _, kind := range []string{"AgentReachable", "ConfigApplied", "WorkersReady"} {
		condition(cluster, kind, metav1.ConditionFalse, "AgentPending", "Waiting for agent observation")
	}
	config := agentConfig(cluster)
	body, err := json.Marshal(config)
	if err != nil {
		return metav1.ConditionFalse, "AgentConfigFailed", "Cannot encode agent configuration"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, AgentURL(cluster.Name, cluster.Namespace)+"/config", bytes.NewReader(body))
	if err != nil {
		return metav1.ConditionFalse, "AgentConfigFailed", "Cannot construct agent request"
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+configToken)
	httpClient := r.AgentClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		condition(cluster, "AgentReachable", metav1.ConditionFalse, "AgentUnreachable", "Agent request failed or timed out")
		return metav1.ConditionFalse, "AgentUnreachable", "Agent request failed or timed out"
	}
	defer func() { _ = response.Body.Close() }()
	condition(cluster, "AgentReachable", metav1.ConditionTrue, "AgentResponding", "Agent HTTP endpoint is reachable")
	if response.StatusCode != http.StatusAccepted {
		message := "Agent rejected desired configuration"
		var failure workload.Failure
		if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure) == nil && failure.Code == "invalid_config" {
			message += ": " + failure.Message[:min(len(failure.Message), 512)]
		}
		condition(cluster, "ConfigApplied", metav1.ConditionFalse, "ConfigRejected", message)
		return metav1.ConditionFalse, "ConfigRejected", message
	}
	var status workload.Status
	if err := json.NewDecoder(io.LimitReader(response.Body, workload.MaxBodyBytes)).Decode(&status); err != nil {
		return metav1.ConditionFalse, "AgentResponseInvalid", "Agent returned an invalid status response"
	}
	now := metav1.Now()
	cluster.Status.LastAgentContact = &now
	cluster.Status.AppliedGeneration = status.AppliedGeneration
	if status.DesiredGeneration == cluster.Generation && status.Model == config.Model {
		for _, worker := range status.Workers {
			if worker.State == workload.Error {
				message := "Worker failed: " + worker.Message[:min(len(worker.Message), 512)]
				condition(cluster, "WorkersReady", metav1.ConditionFalse, "WorkerFailed", message)
				return metav1.ConditionFalse, "WorkerFailed", message
			}
		}
	}
	if status.DesiredGeneration != cluster.Generation || status.AppliedGeneration != cluster.Generation || status.Model != config.Model {
		return metav1.ConditionFalse, "ConfigPending", "Waiting for desired model and generation to become ready"
	}
	condition(cluster, "ConfigApplied", metav1.ConditionTrue, "ConfigCurrent", "Agent has applied the desired generation")
	seen := make(map[string]bool)
	for _, worker := range status.Workers {
		if worker.ID == "" || seen[worker.ID] {
			return metav1.ConditionFalse, "AgentResponseInvalid", "Agent returned duplicate or empty worker IDs"
		}
		seen[worker.ID] = true
		if worker.State == workload.Ready || worker.State == workload.Busy {
			cluster.Status.ReadyWorkers++
		}
	}
	if cluster.Status.ReadyWorkers != *cluster.Spec.WorkersPerVM || len(status.Workers) != int(*cluster.Spec.WorkersPerVM) {
		return metav1.ConditionFalse, "WorkersPending", "Requested workers are provisioning, offline, or failed"
	}
	condition(cluster, "WorkersReady", metav1.ConditionTrue, "WorkersServing", "All requested workers are loaded and healthy")
	return metav1.ConditionTrue, "InferenceReady", "Requested local inference capacity is ready"
}
