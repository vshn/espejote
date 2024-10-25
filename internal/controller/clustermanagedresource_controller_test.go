/*
Copyright 2024.

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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/internal/dynamic"
)

var customresources = []string{
	`{
  "apiVersion": "espejo.appuio.io/v1alpha1",
  "kind": "ClusterManagedResource",
  "metadata": {
    "name": "alice"
  },
  "spec": {
    "context": [],
    "template": ""
  }
}`,
}

var _ = Describe("ClusterManagedResource Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		BeforeEach(func() {
			clt, _ := dynamic.NewForConfig(cfg)

			By("applying the test environment manifests")
			for _, m := range environment {
				Expect(clt.ApplyJson(ctx, m)).NotTo(HaveOccurred())
			}
			By("applying the test custom resource manifests")
			// for _, m := range customresources {
			// 	obj := &unstructured.Unstructured{}
			// 	Expect(json.Unmarshal([]byte(m), obj)).To(Succeed())
			// 	dr, err := clt.ResourceTest("espejo.apuio.io", "ClusterManagedResource", "")
			// 	Expect(err).NotTo(HaveOccurred())
			// 	Expect(dr.Create(ctx, obj, v1.CreateOptions{
			// 		FieldManager: "espejote",
			// 	})).NotTo(HaveOccurred())
			// }
			mr := &espejov1alpha1.ClusterManagedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name: "alice",
				},
				Spec: espejov1alpha1.ClusterManagedResourceSpec{
					Context: []espejov1alpha1.Context{
						{
							Alias:      "service",
							APIVersion: "v1",
							Kind:       "Service",
							Name:       "bob",
							Namespace:  "alpha",
						},
					},
					Template: template,
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: "alice",
			}, mr)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, mr)).To(Succeed())
			}
		})

		AfterEach(func() {
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ClusterManagedResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: cfg,
			}

			_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name: "alice",
				},
			})
			// Expect(err).NotTo(HaveOccurred())
		})
	})
})
