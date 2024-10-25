package converter

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/internal/dynamic"
)

const template = `
local svc = std.extVar('service');

[
  svc[0] {
    metadata+: {
	  labels: {
	    label: 'value',
	  },
	  name: 'copy'
	},
  },
]`

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
}`,
}

var _ = Describe("Helper functions", func() {
	Context("Converting a ManagedResource", func() {

		BeforeEach(func() {
		})

		AfterEach(func() {
		})

		It("should return map from slice", func() {
			dut := []string{
				"alpha",
				"beta",
				"gamma",
				"delta",
			}

			actual := markedFromList(dut)
			Expect(actual).To(Equal(map[string]bool{
				"alpha": false,
				"beta":  false,
				"gamma": false,
				"delta": false,
			}))
		})

		It("should return slice from map", func() {
			dut := map[string]bool{
				"alpha": false,
				"beta":  true,
				"gamma": true,
				"delta": false,
			}

			actual := listFromMarked(dut)
			Expect(actual).To(ContainElements(
				"beta",
				"gamma",
			))
		})

		It("should mark map from slice", func() {
			dut := map[string]bool{
				"alpha": false,
				"beta":  false,
				"gamma": false,
				"delta": false,
			}
			matches := []string{
				"alpha",
				"bet.*",
				".*mma",
			}

			actual := markMatchedNamespaces(matches, dut)
			Expect(actual).To(Equal(map[string]bool{
				"alpha": true,
				"beta":  true,
				"gamma": true,
				"delta": false,
			}))
		})

		It("should unmark map from slice", func() {
			dut := map[string]bool{
				"alpha": false,
				"beta":  true,
				"gamma": true,
				"delta": true,
			}
			matches := []string{
				"alpha",
				"bet.*",
				".*mma",
			}

			actual := unmarkMatchedNamespaces(matches, dut)
			Expect(actual).To(Equal(map[string]bool{
				"alpha": false,
				"beta":  false,
				"gamma": false,
				"delta": true,
			}))
		})
	})
})

var _ = Describe("Helper functions with client", func() {
	Context("Converting a ManagedResource", func() {

		BeforeEach(func() {
			clt, _ := dynamic.NewForConfig(cfg)

			By("applying the test environment manifests")
			for _, m := range environment {
				Expect(clt.ApplyJson(ctx, m)).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
		})

		It("should get all namespaces", func() {
			clt, err := dynamic.NewForConfig(cfg)
			Expect(err).To(Succeed())

			By("list of all namespaces")
			actual, err := getAllNamespaces(ctx, clt)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(ContainElements(
				"alpha",
				"beta",
				"gamma",
				"delta",
				"default",
				"kube-system",
				"kube-public",
				"kube-node-lease",
			))
		})

		// 🤔 No clue how to crete label selectors, it just thorws errors
		// It("should get labeled namespaces", func() {
		// 	clt, err := dynamic.NewForConfig(cfg)
		// 	Expect(err).To(Succeed())

		// 	By("list of blue namespaces")
		// 	// selector := make(map[string]string, 1)
		// 	// selector["colour"] = "blue"
		// 	// actual, err := getLabeledNamespaces(ctx, clt, &metav1.LabelSelector{
		// 	// 	MatchLabels: selector,
		// 	// })
		// 	// Expect(err).NotTo(HaveOccurred())
		// 	// Expect(actual).To(ContainElements(
		// 	// 	"alpha",
		// 	// 	"beta",
		// 	// ))

		// 	By("list of red namespaces")
		// 	// req, _ = labels.NewRequirement("colour", selection.Equals, []string{"blue"})
		// 	// selector = labels.NewSelector()
		// 	// selector.Add(*req)
		// 	// actual, err = getLabeledNamespaces(ctx, clt, metav1.SetAsLabelSelector(labels.Set{
		// 	// 	"colour": "red",
		// 	// }))
		// 	// Expect(err).NotTo(HaveOccurred())
		// 	// Expect(actual).To(ContainElements(
		// 	// 	"gamma",
		// 	// 	"delta",
		// 	// ))
		// })
	})
})

var _ = Describe("ManagedResource Converter", func() {
	Context("Converting a ManagedResource", func() {
		ctx := context.Background()

		BeforeEach(func() {
			clt, _ := dynamic.NewForConfig(cfg)

			By("applying the test environment manifests")
			for _, m := range environment {
				Expect(clt.ApplyJson(ctx, m)).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
		})

		It("should successfully filter namespaces with match names", func() {
			clt, err := dynamic.NewForConfig(cfg)
			Expect(err).To(Succeed())

			By("list of names")
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
							Name:       "original",
							Namespace:  "alice",
						},
					},
					NamespaceSelector: espejov1alpha1.NamespaceSelector{
						MatchNames: []string{"alpha", "^b.*a"},
					},
					Template: template,
				},
			}

			list := &corev1.NamespaceList{}
			Expect(k8sClient.List(ctx, list)).To(Succeed())
			// 4 namespaces from test setup
			// + default
			// + kube-node-lease
			// + kube-public
			// + kube-system
			Expect(list.Items).To(HaveLen(8))

			is, err := convertToNamespaces(ctx, clt, mr)
			Expect(err).NotTo(HaveOccurred())
			Expect(is).To(HaveLen(2))
			Expect(is).To(ContainElements([]string{"alpha", "beta"}))
		})

		It("should successfully filter namespaces with ignore names", func() {
			clt, err := dynamic.NewForConfig(cfg)
			Expect(err).To(Succeed())

			By("list of names")
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
							Name:       "original",
							Namespace:  "alice",
						},
					},
					NamespaceSelector: espejov1alpha1.NamespaceSelector{
						MatchNames:  []string{".*"},
						IgnoreNames: []string{"alpha", "kube.*"},
					},
					Template: template,
				},
			}

			list := &corev1.NamespaceList{}
			Expect(k8sClient.List(ctx, list)).To(Succeed())
			// 4 namespaces from test setup
			// + default
			// + kube-node-lease
			// + kube-public
			// + kube-system
			Expect(list.Items).To(HaveLen(8))

			is, err := convertToNamespaces(ctx, clt, mr)
			Expect(err).NotTo(HaveOccurred())
			Expect(is).To(HaveLen(4))
			Expect(is).To(ContainElements([]string{"beta", "gamma", "default", "delta"}))
		})
	})
})
