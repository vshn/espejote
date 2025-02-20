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

// const template = `
// local svc = std.extVar('service');

// [
//   svc[0] {
//     metadata+: {
// 	  labels: {
// 	    label: 'value',
// 	  },
// 	  name: 'copy'
// 	},
//   },
// ]`

const template = `
local svc = std.extVar('service');

[
  svc[0] {
    metadata: {
      name: 'charly'
      namespace: 'alpha',
    }
  }
]`

// const template = `[
//   {
//     apiVersion: 'v1',
//     kind: 'Service',
//     metadata: {
//       name: 'original',
//       namespace: 'alice',
//     },
//     spec: {
//       type: 'ClusterIP',
//       selector: {
//         label: 'value',
//   	},
//     ports: [{
//         name: 'another-name',
//         port: 8080,
//         protocol: 'TCP',
//         targetPort: 'http',
//   	}],
//     },
//   }
// ]`

// const template = `[
//   {
//     apiVersion: 'v1',
//     kind: 'Service',
//     metadata: {
//       name: 'original',
//       namespace: 'alice',
//     },
//     spec: {
//       type: 'ClusterIP',
//       selector: {
//         label: 'value',
//   	},
//     ports: [{
//         name: 'another-name',
//         port: 8080,
//         protocol: 'TCP',
//         targetPort: 'http',
//   	}],
//     },
//   }
// ]`

// const service = `
// {
//   apiVersion: 'v1',
//   kind: 'Service',
//   metadata: {
//     name: 'original',
//     namespace: 'alice',
//   },
//   spec: {
//     type: 'ClusterIP',
//     selector: {
//       label: 'value',
// 	},
//     ports: [{
//       name: 'port-name',
//       port: 8080,
//       protocol: 'TCP',
//       targetPort: 'http',
// 	}],
//   },
// }
// `

var environment = []string{
	`{
  "apiVersion": "v1",
  "kind": "Namespace",
  "metadata": {
    "labels": {
      "colour": "blue"
    },
    "name": "alpha"
  }
}`, `{
  "apiVersion": "v1",
  "kind": "Namespace",
  "metadata": {
    "labels": {
      "colour": "blue"
    },
    "name": "beta"
  }
}`, `{
  "apiVersion": "v1",
  "kind": "Namespace",
  "metadata": {
    "labels": {
      "colour": "red"
    },
    "name": "gamma"
  }
}`, `{
  "apiVersion": "v1",
  "kind": "Namespace",
  "metadata": {
    "labels": {
    "colour": "red"
  },
    "name": "delta"
  }
}`, `{
  "apiVersion": "v1",
  "kind": "Service",
  "metadata": {
    "name": "bob",
    "namespace": "alpha"
  },
  "spec": {
    "type": "ClusterIP",
    "selector": {
      "label": "value"
    },
    "ports": [{
      "name": "port-name",
      "port": 8080,
      "protocol": "TCP",
      "targetPort": "http"
    }]
  }
}`,
}

// "apiVersion": "espejo.appuio.io/v1alpha1",
// "kind": "ManagedResource",
// "metadata": {
//   "name": "alice",
//   "namespace": "alpha"
// }
// }`, `{

var _ = Describe("ManagedResource Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		BeforeEach(func() {
			clt, _ := dynamic.NewForConfig(cfg)

			By("applying the test environment manifests")
			for _, m := range environment {
				Expect(clt.ApplyJson(ctx, m)).NotTo(HaveOccurred())
			}

			By("creating the customresources")
			mr := &espejov1alpha1.ManagedResource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alice",
					Namespace: "alpha",
				},
				Spec: espejov1alpha1.ManagedResourceSpec{
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
				Name:      "alice",
				Namespace: "alpha",
			}, mr)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, mr)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			// resource := &espejov1alpha1.ManagedResource{}
			// err := k8sClient.Get(ctx, typeNamespacedName, resource)
			// Expect(err).NotTo(HaveOccurred())

			// By("Cleanup the specific resource instance ManagedResource")
			// Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ManagedResourceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: cfg,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "alice",
					Namespace: "alpha",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking generated resource")

			// is := &corev1.Service{}
			// Expect(k8sClient.Get(ctx, types.NamespacedName{
			// 	Name:      "charly",
			// 	Namespace: "alpha",
			// }, is)).To(Succeed())
			// Expect(is.Spec.Type).To(Equal(is))
		})
	})
})
