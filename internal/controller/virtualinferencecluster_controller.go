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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

// VirtualInferenceClusterReconciler reconciles a VirtualInferenceCluster object
type VirtualInferenceClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.vetch.io,resources=virtualinferenceclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.vetch.io,resources=virtualinferenceclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.vetch.io,resources=virtualinferenceclusters/finalizers,verbs=update

// Reconcile reads the requested cluster and logs its desired node count.
func (r *VirtualInferenceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cluster := &infrastructurev1alpha1.VirtualInferenceCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		// A deleted cluster no longer needs to be reconciled.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Observed VirtualInferenceCluster", "namespace", cluster.Namespace,
		"name", cluster.Name, "desiredNodes", cluster.Spec.Nodes)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualInferenceClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrastructurev1alpha1.VirtualInferenceCluster{}).
		Named("virtualinferencecluster").
		Complete(r)
}
