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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	infrastructurev1alpha1 "github.com/Sao-Ali/vetch/api/v1alpha1"
)

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
				Namespace:    "default",
			},
			Spec: infrastructurev1alpha1.VirtualInferenceClusterSpec{Nodes: 2},
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
		current.Spec.Nodes = 4
		Expect(k8sClient.Update(testContext, current)).To(Succeed())
		_, err = reconciler.Reconcile(testContext, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownedConfigMaps(testContext, current)).To(HaveLen(4))

		current = getCluster(testContext, client.ObjectKeyFromObject(cluster))
		current.Spec.Nodes = 1
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
})

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
