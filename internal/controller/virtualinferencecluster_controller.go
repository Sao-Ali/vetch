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
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

const (
	availableConditionType = "Available"
	componentLabelKey      = "infrastructure.vetch.io/component"
	clusterUIDLabelKey     = "infrastructure.vetch.io/cluster-uid"
	nodeIndexLabelKey      = "infrastructure.vetch.io/node-index"
	dummyNodeComponent     = "dummy-node"
	maxObjectNameLength    = 253
)

// VirtualInferenceClusterReconciler reconciles a VirtualInferenceCluster object.
type VirtualInferenceClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.vetch.io,resources=virtualinferenceclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=infrastructure.vetch.io,resources=virtualinferenceclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;delete

// Reconcile makes the owned dummy-node ConfigMaps match the requested node count.
func (r *VirtualInferenceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cluster := &infrastructurev1alpha1.VirtualInferenceCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		// A deleted cluster no longer needs to be reconciled.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling VirtualInferenceCluster", "desiredNodes", cluster.Spec.Nodes)

	if err := r.reconcileDummyNodes(ctx, cluster); err != nil {
		conditionErr := r.setAvailableCondition(ctx, cluster, metav1.ConditionFalse,
			"ReconciliationFailed", err.Error())
		return ctrl.Result{}, errors.Join(err, conditionErr)
	}

	message := fmt.Sprintf("All %d dummy nodes are available", cluster.Spec.Nodes)
	if err := r.setAvailableCondition(ctx, cluster, metav1.ConditionTrue, "Reconciled", message); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *VirtualInferenceClusterReconciler) reconcileDummyNodes(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
) error {
	desiredNames := make(map[string]struct{}, cluster.Spec.Nodes)
	for nodeIndex := int32(0); nodeIndex < cluster.Spec.Nodes; nodeIndex++ {
		name := dummyNodeName(cluster.Name, nodeIndex)
		desiredNames[name] = struct{}{}
		if err := r.reconcileDummyNode(ctx, cluster, nodeIndex, name); err != nil {
			return err
		}
	}

	configMaps := &corev1.ConfigMapList{}
	if err := r.List(ctx, configMaps, client.InNamespace(cluster.Namespace)); err != nil {
		return fmt.Errorf("list ConfigMaps: %w", err)
	}

	for i := range configMaps.Items {
		configMap := &configMaps.Items[i]
		if !metav1.IsControlledBy(configMap, cluster) {
			continue
		}
		if _, desired := desiredNames[configMap.Name]; desired {
			continue
		}
		if err := r.Delete(ctx, configMap); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete dummy node %q: %w", configMap.Name, err)
		}
	}

	return nil
}

func (r *VirtualInferenceClusterReconciler) reconcileDummyNode(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
	nodeIndex int32,
	name string,
) error {
	configMap := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: cluster.Namespace, Name: name}
	if err := r.Get(ctx, key, configMap); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get dummy node %q: %w", name, err)
		}

		configMap = desiredDummyNode(cluster, nodeIndex, name)
		if err := controllerutil.SetControllerReference(cluster, configMap, r.Scheme); err != nil {
			return fmt.Errorf("set owner for dummy node %q: %w", name, err)
		}
		if err := r.Create(ctx, configMap); err != nil {
			return fmt.Errorf("create dummy node %q: %w", name, err)
		}
		return nil
	}

	if !metav1.IsControlledBy(configMap, cluster) {
		return fmt.Errorf("ConfigMap %q already exists and is not controlled by VirtualInferenceCluster %q", name, cluster.Name)
	}

	original := configMap.DeepCopy()
	applyDummyNodeFields(configMap, cluster, nodeIndex)
	if equality.Semantic.DeepEqual(original, configMap) {
		return nil
	}
	if err := r.Update(ctx, configMap); err != nil {
		return fmt.Errorf("update dummy node %q: %w", name, err)
	}
	return nil
}

func desiredDummyNode(
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
	nodeIndex int32,
	name string,
) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
		},
	}
	applyDummyNodeFields(configMap, cluster, nodeIndex)
	return configMap
}

func applyDummyNodeFields(
	configMap *corev1.ConfigMap,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
	nodeIndex int32,
) {
	if configMap.Labels == nil {
		configMap.Labels = make(map[string]string)
	}
	configMap.Labels["app.kubernetes.io/managed-by"] = "vetch"
	configMap.Labels[componentLabelKey] = dummyNodeComponent
	configMap.Labels[clusterUIDLabelKey] = string(cluster.UID)
	configMap.Labels[nodeIndexLabelKey] = strconv.FormatInt(int64(nodeIndex), 10)

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data["cluster"] = cluster.Name
	configMap.Data["nodeIndex"] = strconv.FormatInt(int64(nodeIndex), 10)
}

func dummyNodeName(clusterName string, nodeIndex int32) string {
	suffix := "-node-" + strconv.FormatInt(int64(nodeIndex), 10)
	if len(clusterName)+len(suffix) <= maxObjectNameLength {
		return clusterName + suffix
	}

	hash := sha256.Sum256([]byte(clusterName))
	hashText := fmt.Sprintf("%x", hash[:5])
	prefixLength := maxObjectNameLength - len(suffix) - len(hashText) - 1
	prefix := strings.TrimRight(clusterName[:prefixLength], ".-")
	return prefix + "-" + hashText + suffix
}

func (r *VirtualInferenceClusterReconciler) setAvailableCondition(
	ctx context.Context,
	cluster *infrastructurev1alpha1.VirtualInferenceCluster,
	status metav1.ConditionStatus,
	reason string,
	message string,
) error {
	original := cluster.DeepCopy()
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               availableConditionType,
		Status:             status,
		ObservedGeneration: cluster.Generation,
		Reason:             reason,
		Message:            message,
	})
	if equality.Semantic.DeepEqual(original.Status, cluster.Status) {
		return nil
	}
	if err := r.Status().Update(ctx, cluster); err != nil {
		return fmt.Errorf("update VirtualInferenceCluster status: %w", err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualInferenceClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.VirtualInferenceCluster{}).
		Owns(&corev1.ConfigMap{}).
		Named("virtualinferencecluster").
		Complete(r)
}
