package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

const (
	configTokenKey             = "config-token"
	bootstrapUIDAnnotation     = "infrastructure.vetch.io/agent-bootstrap-uid"
	defaultControllerNamespace = "vetch-system"
)

func agentSecretName(name string) string { return AgentServiceName(name) + "-auth" }

func bootstrapUserData(token string) string {
	return "#cloud-config\nwrite_files:\n" +
		"  - path: /etc/vetch/config.json\n    permissions: '0644'\n    content: |\n" +
		"      {\"model\":\"bootstrap\",\"workersPerVM\":1}\n" +
		"  - path: /etc/vetch/config-token\n    owner: root:vetch\n    permissions: '0440'\n    content: '" + token + "'\n"
}

func (r *VirtualInferenceClusterReconciler) reconcileAgentAccess(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	key := client.ObjectKey{Namespace: cluster.Namespace, Name: agentSecretName(cluster.Name)}
	if err := r.Get(ctx, key, secret); apierrors.IsNotFound(err) {
		credential := make([]byte, 32)
		if _, err := rand.Read(credential); err != nil {
			return nil, fmt.Errorf("generate agent credential: %w", err)
		}
		token := hex.EncodeToString(credential)
		immutable := true
		secret = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}, Type: corev1.SecretTypeOpaque,
			Immutable: &immutable, Data: map[string][]byte{configTokenKey: []byte(token), "userdata": []byte(bootstrapUserData(token))}}
		if err := controllerutil.SetControllerReference(cluster, secret, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Create(ctx, secret); err != nil {
			return nil, fmt.Errorf("create agent bootstrap Secret: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("get agent bootstrap Secret: %w", err)
	}
	if !metav1.IsControlledBy(secret, cluster) {
		return nil, fmt.Errorf("agent bootstrap Secret is not controlled by this VirtualInferenceCluster")
	}
	token, err := hex.DecodeString(string(secret.Data[configTokenKey]))
	if err != nil || len(token) != 32 || secret.Immutable == nil || !*secret.Immutable ||
		string(secret.Data["userdata"]) != bootstrapUserData(string(secret.Data[configTokenKey])) {
		return nil, fmt.Errorf("agent bootstrap Secret is invalid; restore it or rotate it and restart the VM")
	}
	if err := r.reconcileAgentPolicy(ctx, cluster); err != nil {
		return nil, err
	}
	return secret, nil
}

func (r *VirtualInferenceClusterReconciler) reconcileAgentPolicy(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster) error {
	policy := &networkingv1.NetworkPolicy{}
	key := client.ObjectKey{Namespace: cluster.Namespace, Name: AgentServiceName(cluster.Name)}
	err := r.Get(ctx, key, policy)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	create := apierrors.IsNotFound(err)
	if create {
		policy.ObjectMeta = metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}
		if err := controllerutil.SetControllerReference(cluster, policy, r.Scheme); err != nil {
			return err
		}
	} else if !metav1.IsControlledBy(policy, cluster) {
		return fmt.Errorf("agent NetworkPolicy is not controlled by this VirtualInferenceCluster")
	}
	original := policy.DeepCopy()
	namespace := r.ControllerNamespace
	if namespace == "" {
		namespace = defaultControllerNamespace
	}
	port, protocol := intstr.FromInt32(8080), corev1.ProtocolTCP
	policy.Spec = networkingv1.NetworkPolicySpec{
		PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{clusterUIDLabelKey: string(cluster.UID), componentLabelKey: vmComponent}},
		PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
		Ingress: []networkingv1.NetworkPolicyIngressRule{{Ports: []networkingv1.NetworkPolicyPort{{Port: &port, Protocol: &protocol}},
			From: []networkingv1.NetworkPolicyPeer{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
					PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"control-plane": "controller-manager", "app.kubernetes.io/name": applicationName}}},
				{PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "vetch-gateway"}}},
			}}},
	}
	if create {
		return r.Create(ctx, policy)
	}
	if equality.Semantic.DeepEqual(original.Spec, policy.Spec) {
		return nil
	}
	return r.Update(ctx, policy)
}

func (r *VirtualInferenceClusterReconciler) cleanupInactiveAgent(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster, reason string) error {
	if *cluster.Spec.VMCount != 0 && cluster.Spec.Worker != nil {
		return nil
	}
	if *cluster.Spec.VMCount == 0 {
		meta.RemoveStatusCondition(&cluster.Status.Conditions, "VMReady")
	}
	if err := r.deleteOwnedAgentService(ctx, cluster); err != nil {
		return err
	}
	if reason != vmDeletionPending {
		return r.deleteOwnedAgentAccess(ctx, cluster)
	}
	return nil
}

func (r *VirtualInferenceClusterReconciler) deleteOwnedAgentAccess(ctx context.Context, cluster *infrastructurev1alpha1.VirtualInferenceCluster) error {
	for _, object := range []client.Object{&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: agentSecretName(cluster.Name), Namespace: cluster.Namespace}},
		&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: AgentServiceName(cluster.Name), Namespace: cluster.Namespace}}} {
		if err := r.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if metav1.IsControlledBy(object, cluster) {
			uid := object.GetUID()
			if err := r.Delete(ctx, object, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}
