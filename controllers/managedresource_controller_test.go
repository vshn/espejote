package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	metricserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/testutil"
)

// Test_ManagedResourceReconciler_Reconcile tests the ManagedResourceReconciler.
// For efficiency, the tests are run in parallel and there is only one instance of the controller and api-server.
// It is in the responsibility of the test to ensure that the resources do not conflict with each other.
// Tests can use the `testutil.TmpNamespace()“ function to create a new namespace that is guaranteed to not conflict with namespaces of other tests.
// Special care must be taken when modifying cluster scoped resources, for example by prefixing the resource names with the name returned from `testutil.TmpNamespace()`.
func Test_ManagedResourceReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme, cfg := testutil.SetupEnvtestEnv(t)
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	ctx := log.IntoContext(t.Context(), testr.New(t))

	metricsPort, err := freePort()
	t.Log("metrics port:", metricsPort)
	require.NoError(t, err)
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricserver.Options{
			BindAddress: ":" + strconv.Itoa(metricsPort),
		},
		Logger: testr.New(t),
	})
	require.NoError(t, err)
	subject := &ManagedResourceControllerManager{
		Client:                  c,
		Scheme:                  c.Scheme(),
		ControllerLifetimeCtx:   ctx,
		JsonnetLibraryNamespace: "jsonnetlibs",
		Recorder:                mgr.GetEventRecorderFor("managed-resource-controller"),
	}
	require.NoError(t, subject.SetupWithManager("managedresource", cfg, mgr))
	metrics.Registry.MustRegister(&CacheSizeCollector{ControllerManager: subject})

	mgrCtx, mgrCancel := context.WithCancel(ctx)
	t.Cleanup(mgrCancel)
	go func() {
		require.NoError(t, mgr.Start(mgrCtx))
	}()

	t.Run("(relative) jsonnet imports", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		jsonnetLibNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: subject.JsonnetLibraryNamespace,
			},
		}
		require.NoError(t, c.Create(ctx, jsonnetLibNs))
		jsonnetLib := &espejotev1alpha1.JsonnetLibrary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: subject.JsonnetLibraryNamespace,
			},
			Spec: espejotev1alpha1.JsonnetLibrarySpec{
				Data: map[string]string{
					"test.jsonnet":       `import "dotrel.jsonnet"`,
					"dotrel.jsonnet":     `import "./relrel.jsonnet"`,
					"relrel.jsonnet":     `import "rel.jsonnet"`,
					"espejote.libsonnet": `"wo"`,
					"rel.jsonnet":        `{hello: (import "./espejote.libsonnet")+(import "test2/test.jsonnet")+"-"+(import "espejote.libsonnet").triggerName()}`,
				},
			},
		}
		require.NoError(t, c.Create(ctx, jsonnetLib))
		jsonnetLib2 := &espejotev1alpha1.JsonnetLibrary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test2",
				Namespace: subject.JsonnetLibraryNamespace,
			},
			Spec: espejotev1alpha1.JsonnetLibrarySpec{
				Data: map[string]string{
					"test.jsonnet":   `import "relrel.jsonnet"`,
					"relrel.jsonnet": `import "rel.jsonnet"`,
					"rel.jsonnet":    `"rld"`,
				},
			},
		}
		require.NoError(t, c.Create(ctx, jsonnetLib2))
		localJsonnetLib := &espejotev1alpha1.JsonnetLibrary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.JsonnetLibrarySpec{
				Data: map[string]string{
					"test.jsonnet":   `import "relrel.jsonnet"`,
					"relrel.jsonnet": `import "rel.jsonnet"`,
					"rel.jsonnet":    `{hello: (import "test2/test.jsonnet")+(import "lib/test/test.jsonnet").hello}`,
				},
			},
		}
		require.NoError(t, c.Create(ctx, localJsonnetLib))
		localJsonnetLib2 := &espejotev1alpha1.JsonnetLibrary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test2",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.JsonnetLibrarySpec{
				Data: map[string]string{
					"test.jsonnet":   `import "relrel.jsonnet"`,
					"relrel.jsonnet": `import "rel.jsonnet"`,
					"rel.jsonnet":    `"local-"`,
				},
			},
		}
		require.NoError(t, c.Create(ctx, localJsonnetLib2))

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						Name: "ns",
						WatchResource: espejotev1alpha1.TriggerWatchResource{
							Kind:       "Namespace",
							APIVersion: "v1",
							Name:       testns + "-2",
						},
					},
				},
				Template: `
local esp = import "espejote.libsonnet";
local test = import "lib/test/test.jsonnet";
local localTest = import "test/test.jsonnet";
local trigger = esp.triggerData();

if esp.triggerName() == "ns" then [{
	apiVersion: 'v1',
	kind: 'ConfigMap',
	metadata: {
		name: 'test',
		namespace: trigger.resource.metadata.name,
		annotations: {
			"espejote.vshn.net/hello": test.hello,
			"espejote.vshn.net/local-hello": localTest.hello,
		},
	},
}]
				`,
			},
		}
		require.NoError(t, c.Create(ctx, res))

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testns + "-2",
			},
		}
		require.NoError(t, c.Create(ctx, ns))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: ns.Name, Name: "test"}, &cm))
			assert.Equal(t, "world-ns", cm.Annotations["espejote.vshn.net/hello"])
			assert.Equal(t, "local-world-ns", cm.Annotations["espejote.vshn.net/local-hello"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("managed resource deletion", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		otherCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		require.NoError(t, c.Create(ctx, otherCM))

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "configmap",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						Name:       "other",
					},
				}},
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						Name: "configmap",
						WatchContextResource: espejotev1alpha1.WatchContextResource{
							Name: "configmap",
						},
					},
				},
				Template: `[{
					apiVersion: 'v1',
					kind: 'ConfigMap',
					metadata: {
						name: 'copied',
					},
					data: (import "espejote.libsonnet").context().configmap[0].data,
				}]
				`,
			},
		}
		require.NoError(t, c.Create(ctx, res))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "copied"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("managed resource in deletion state should stop reconciling")
		require.NoError(t, c.Patch(ctx, res, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":["go.test/finalizer"]}}`))))
		require.NoError(t, c.Delete(ctx, res))

		require.NoError(t, c.Patch(ctx, res, client.RawPatch(types.MergePatchType, []byte(`{"data":{"test":"updated"}}`))))

		require.Never(t, func() bool {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "copied"}, &cm))
			return cm.Data["test"] != "test"
		}, 2*time.Second, 100*time.Millisecond, "managed resource should stop reconciling if there is a deletion timestamp")

		require.NoError(t, c.Patch(ctx, res, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"finalizers":null}}`))))
	})

	t.Run("reconcile from added watch resource trigger", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "configmaps",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion:  "v1",
						Kind:        "ConfigMap",
						IgnoreNames: []string{"collected"},
					},
				}},
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						Name: "configmap",
						WatchContextResource: espejotev1alpha1.WatchContextResource{
							Name: "configmaps",
						},
					},
				},
				Template: `
local esp = import "espejote.libsonnet";
local trigger = esp.triggerData();

[{
  apiVersion: 'v1',
  kind: 'ConfigMap',
  metadata: {
    name: 'collected',
  },
  data: {
    collected: std.manifestJsonMinified(std.map(function(cm) cm.metadata.name, esp.context().configmaps)),
  },
}]
				`,
			},
		}
		require.NoError(t, c.Create(ctx, res))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &cm))

			var cms []string
			require.NoError(t, json.Unmarshal([]byte(cm.Data["collected"]), &cms))
			assert.ElementsMatch(t, []string{}, cms)
		}, 5*time.Second, 100*time.Millisecond)

		for i := range 3 {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test" + strconv.Itoa(i),
					Namespace: testns,
				},
			}
			require.NoError(t, c.Create(ctx, cm))
		}
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &cm))

			var cms []string
			require.NoError(t, json.Unmarshal([]byte(cm.Data["collected"]), &cms))
			assert.ElementsMatch(t, []string{"test0", "test1", "test2"}, cms)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("reconfigure filtered contexts", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		for i := range 500 {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test" + strconv.Itoa(i),
					Namespace: testns,
				},
			}
			require.NoError(t, c.Create(ctx, cm))
		}

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "cms",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion:  "v1",
						Kind:        "ConfigMap",
						IgnoreNames: []string{"collected", "test1", "test3"},
					},
				}},
				Template: `
local esp = import "espejote.libsonnet";
local cms = esp.context()["cms"];

[{
	apiVersion: 'v1',
	kind: 'ConfigMap',
	metadata: {
		name: 'collected',
	},
	data: {
		cms: std.manifestJsonMinified(std.map(function(cm) cm.metadata.name, cms)),
	}
}]
				`,
			},
		}
		require.NoError(t, c.Create(ctx, res))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &cm))

			var cms []string
			require.NoError(t, json.Unmarshal([]byte(cm.Data["cms"]), &cms))
			expected := make([]string, 0, 500)
			for i := 0; i < 500; i++ {
				if i == 1 || i == 3 {
					continue
				}
				expected = append(expected, "test"+strconv.Itoa(i))
			}
			assert.ElementsMatch(t, expected, cms)
		}, 5*time.Second, 100*time.Millisecond)

		var resToUpdate espejotev1alpha1.ManagedResource
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: res.Name}, &resToUpdate))
		resToUpdate.Spec.Triggers = []espejotev1alpha1.ManagedResourceTrigger{{
			Name: "cms",
			WatchResource: espejotev1alpha1.TriggerWatchResource{
				Kind:       "ConfigMap",
				APIVersion: "v1",
				MatchNames: []string{"test1", "test3"},
			},
		}}
		resToUpdate.Spec.Context[0].Resource.IgnoreNames = []string{}
		resToUpdate.Spec.Context[0].Resource.MatchNames = []string{"test1", "test3"}
		require.NoError(t, c.Update(ctx, &resToUpdate))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &cm))

			var cms []string
			require.NoError(t, json.Unmarshal([]byte(cm.Data["cms"]), &cms))
			assert.ElementsMatch(t, []string{"test1", "test3"}, cms)
		}, 5*time.Second, 100*time.Millisecond)

		var cmToDelete corev1.ConfigMap
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test1"}, &cmToDelete))
		require.NoError(t, c.Delete(ctx, &cmToDelete))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &cm))

			var cms []string
			require.NoError(t, json.Unmarshal([]byte(cm.Data["cms"]), &cms))
			assert.ElementsMatch(t, []string{"test3"}, cms)
		}, 5*time.Second, 100*time.Millisecond)

		require.NoError(t, c.Delete(ctx, res))

		var cmToDelete2 corev1.ConfigMap
		require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test3"}, &cmToDelete2))
		require.NoError(t, c.Delete(ctx, &cmToDelete2))

		require.Never(t, func() bool {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &cm))
			var cms []string
			require.NoError(t, json.Unmarshal([]byte(cm.Data["cms"]), &cms))
			return len(cms) < 1
		}, 2*time.Second, 100*time.Millisecond, "should stop reconciling after deletion")
	})

	t.Run("resource outside core group", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		for i := range 3 {
			cm := &networkingv1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test" + strconv.Itoa(i),
					Namespace: testns,
				},
			}
			require.NoError(t, c.Create(ctx, cm))
		}

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "netpols",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion:  "networking.k8s.io/v1",
						Kind:        "NetworkPolicy",
						IgnoreNames: []string{"collected"},
					},
				}},
				Template: `
local esp = import "espejote.libsonnet";
local netpols = esp.context().netpols;

[{
	apiVersion: 'networking.k8s.io/v1',
	kind: 'NetworkPolicy',
	metadata: {
		name: 'collected',
		annotations: {
			netpols: std.manifestJsonMinified(std.map(function(np) np.metadata.name, netpols)),
		},
	},
}]
				`,
			},
		}
		require.NoError(t, c.Create(ctx, res))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var np networkingv1.NetworkPolicy
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "collected"}, &np))

			var cms []string
			require.NoError(t, json.Unmarshal([]byte(np.GetAnnotations()["netpols"]), &cms))
			assert.ElementsMatch(t, []string{"test0", "test1", "test2"}, cms)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("drop nulls from returned list", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `[null, {apiVersion: "v1", kind: "ConfigMap", metadata: {name: "test"}} , null]`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 0)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("template error", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `glug`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, TemplateError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("attach error to triggering object", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `glug`,
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						Name: "cm",
						WatchResource: espejotev1alpha1.TriggerWatchResource{
							Kind:       "ConfigMap",
							APIVersion: "v1",
						},
					},
				},
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "trigger",
				Namespace: testns,
			},
		}
		require.NoError(t, c.Create(ctx, cm))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, TemplateError)

			var triggeringObjEvents corev1.EventList
			require.NoError(t, c.List(ctx, &triggeringObjEvents, client.InNamespace(testns), eventSelectorFor("ConfigMap", cm.Name)))
			require.Len(t, triggeringObjEvents.Items, 1)
			assert.Equal(t, "Warning", triggeringObjEvents.Items[0].Type)
			assert.Contains(t, triggeringObjEvents.Items[0].Message, TemplateError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("duplicate context definition", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{
					{Name: "test"},
					{Name: "test"},
				},
				Template: ``,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, DependencyConfigurationError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("duplicate trigger definition", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{Name: "test"},
					{Name: "test"},
				},
				Template: ``,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, DependencyConfigurationError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("service account does not exist", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `[]`,
				ServiceAccountRef: corev1.LocalObjectReference{
					Name: "does-not-exist",
				},
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ServiceAccountError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("invalid template return", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `"glug"`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, TemplateReturnError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("object with unknown api returned", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{apiVersion: "v1", kind: "DoesNotExist"}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("trigger api is not registered", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						Name: "invalid",
						WatchResource: espejotev1alpha1.TriggerWatchResource{
							Kind:       "WhatWouldThisControllerDo",
							APIVersion: "v1",
						},
					},
				},
				Template: `[]`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, DependencyConfigurationError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("force ownership", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		cmToPatch := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		const origOwner = "other-owner"
		require.NoError(t, c.Patch(ctx, cmToPatch, client.Apply, client.FieldOwner(origOwner)))

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the conflict event")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
			assert.Contains(t, events.Items[0].Message, "conflict")
			assert.Contains(t, events.Items[0].Message, origOwner)
			assert.Contains(t, events.Items[0].Message, ".data.test")

			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("force updating the resource and waiting for a successful update")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.ApplyOptions.Force = true
		require.NoError(t, c.Update(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "updated", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("force ownership espejote.libsonnet", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		cmToPatch := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		const origOwner = "other-owner"
		require.NoError(t, c.Patch(ctx, cmToPatch, client.Apply, client.FieldOwner(origOwner)))

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the conflict event")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
			assert.Contains(t, events.Items[0].Message, "conflict")
			assert.Contains(t, events.Items[0].Message, origOwner)
			assert.Contains(t, events.Items[0].Message, ".data.test")

			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("force updating the resource and waiting for a successful update")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.Template = `(import "espejote.libsonnet").applyOptions({
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}, force=true)`
		require.NoError(t, c.Update(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "updated", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("override field manager", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		cmToPatch := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		const origOwner = "other-owner"
		require.NoError(t, c.Patch(ctx, cmToPatch, client.Apply, client.FieldOwner(origOwner)))

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the conflict event")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
			assert.Contains(t, events.Items[0].Message, "conflict")
			assert.Contains(t, events.Items[0].Message, origOwner)
			assert.Contains(t, events.Items[0].Message, ".data.test")

			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("changing the field manager to the original field manager and waiting for a successful update")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.ApplyOptions.FieldManager = origOwner
		require.NoError(t, c.Update(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "updated", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("override field manager espejote.libsonnet", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		cmToPatch := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		const origOwner = "other-owner"
		require.NoError(t, c.Patch(ctx, cmToPatch, client.Apply, client.FieldOwner(origOwner)))

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the conflict event")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
			assert.Contains(t, events.Items[0].Message, "conflict")
			assert.Contains(t, events.Items[0].Message, origOwner)
			assert.Contains(t, events.Items[0].Message, ".data.test")

			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("changing the field manager to the original field manager and waiting for a successful update")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.Template = `(import "espejote.libsonnet").applyOptions({
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}, fieldManager="` + origOwner + `")`
		require.NoError(t, c.Update(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "updated", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("espejote.libsonnet#applyOptions(fieldManagerSuffix)", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `[
					{
						apiVersion: "v1",
						kind: "ConfigMap",
						metadata: {
							name: "test",
							namespace: "` + testns + `",
						},
						data: {
							test: "test",
						},
					}, (import "espejote.libsonnet").applyOptions({
						apiVersion: "v1",
						kind: "ConfigMap",
						metadata: {
							name: "test",
							namespace: "` + testns + `",
							annotations: {
								"my.tool/status": "Managed",
							},
						},
					}, fieldManagerSuffix=":status-report")
				]`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
			assert.Equal(t, "Managed", cm.Annotations["my.tool/status"])

			fieldManagers := make([]string, 0, len(cm.ObjectMeta.ManagedFields))
			for _, mf := range cm.ObjectMeta.ManagedFields {
				fieldManagers = append(fieldManagers, mf.Manager)
			}
			assert.Contains(t, fieldManagers, "managed-resource:test")
			assert.Contains(t, fieldManagers, "managed-resource:test:status-report")

		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("field validation Ignore", func(t *testing.T) {
		t.Parallel()
		t.Log("field validation Ignore seems to have no effect on the apply process. This test is here to document this behavior and see if the behavior changes in the future.")

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "test",
					},
					typo: "what what",
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the validation error event")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
			assert.Contains(t, events.Items[0].Message, ".typo")
			assert.Contains(t, events.Items[0].Message, "field not declared")
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("ignoring dropped field and waiting for a successful update")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.ApplyOptions.FieldValidation = "Ignore"
		require.NoError(t, c.Update(ctx, mr))

		// Error should always be IsNotFound, see head of test as for why
		require.Never(t, func() bool {
			return !apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, new(corev1.ConfigMap)))
		}, 2*time.Second, 100*time.Millisecond)
	})

	t.Run("field validation Ignore espejote.libsonnet", func(t *testing.T) {
		t.Parallel()
		t.Log("field validation Ignore seems to have no effect on the apply process. This test is here to document this behavior and see if the behavior changes in the future.")

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "test",
					},
					typo: "what what",
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the validation error event")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
			assert.Contains(t, events.Items[0].Message, ".typo")
			assert.Contains(t, events.Items[0].Message, "field not declared")
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("ignoring dropped field and waiting for a successful update")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.Template = `(import "espejote.libsonnet").applyOptions({
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "test",
					},
					typo: "what what",
				}, fieldValidation="Ignore")`
		require.NoError(t, c.Update(ctx, mr))

		// Error should always be IsNotFound, see head of test as for why
		require.Never(t, func() bool {
			return !apierrors.IsNotFound(c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, new(corev1.ConfigMap)))
		}, 2*time.Second, 100*time.Millisecond)
	})

	t.Run("interval trigger", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		cmToPatch := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		require.NoError(t, c.Create(ctx, cmToPatch))

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						Name:     "interval",
						Interval: metav1.Duration{Duration: 10 * time.Millisecond},
					},
				},
				ApplyOptions: espejotev1alpha1.ApplyOptions{
					Force: true,
				},
				Template: `{
					apiVersion: "v1",
					kind: "ConfigMap",
					metadata: {
						name: "test",
						namespace: "` + testns + `",
					},
					data: {
						test: "updated",
					},
				}`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		t.Log("waiting for the update")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "updated", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("resetting the data. repeating until no conflict")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			cm.Data["test"] = "test"
			require.NoError(t, c.Update(ctx, &cm))
		}, 5*time.Second, time.Millisecond)
		t.Log("waiting for the update")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "updated", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("removing the trigger - test shutdown")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.Triggers = nil
		mr.Spec.Template = `{
			apiVersion: "v1",
			kind: "ConfigMap",
			metadata: {
				name: "test",
				namespace: "` + testns + `",
			},
			data: {
				test: "updated",
				processed: "true",
			},
		}`
		require.NoError(t, c.Update(ctx, mr))

		t.Log("waiting the reconcile/ reconfiguration to finish")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "true", cm.Data["processed"])
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("resetting the data")
		{
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			cm.Data["test"] = "test"
			require.NoError(t, c.Update(ctx, &cm))
		}

		t.Log("waiting for the update to be reflected in the client cache, note that the client cache does not guarantee any read after write consistency")
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			assert.Equal(t, "test", cm.Data["test"])
		}, 5*time.Second, 100*time.Millisecond)
		require.Never(t, func() bool {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			if cm.Data["test"] != "test" {
				t.Logf("test data is %q, expected %q", cm.Data["test"], "test")
				return true
			}
			return false
		}, 2*time.Second, 10*time.Millisecond, "Trigger should be shut down and thus stop updating the resource")
	})

	t.Run("object deletion", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		for i := range 4 {
			cm := &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "ConfigMap",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test" + strconv.Itoa(i),
					Namespace: testns,
					Annotations: map[string]string{
						"index": strconv.Itoa(i),
					},
					Labels: map[string]string{
						"managed": "true",
					},
				},
				Data: map[string]string{
					"test": "test",
				},
			}
			require.NoError(t, c.Create(ctx, cm))
		}

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "cms",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"managed": "true"},
						},
					},
				}},
				ApplyOptions: espejotev1alpha1.ApplyOptions{
					Force: true,
				},
				Template: `
local esp = import 'espejote.libsonnet';

local cms = esp.context().cms;

std.map(
  function(obj)
    if std.parseInt(obj.metadata.annotations.index) == 0 then
      esp.markForDelete(obj, gracePeriodSeconds=0, propagationPolicy="Background")
    else if std.parseInt(obj.metadata.annotations.index) == 1 then
      esp.markForDelete(obj, preconditionUID=obj.metadata.uid)
    else if std.parseInt(obj.metadata.annotations.index) == 2 then
      esp.markForDelete(obj, preconditionResourceVersion=obj.metadata.resourceVersion)
    else
      {
        apiVersion: obj.apiVersion,
        kind: obj.kind,
        metadata: {
          name: obj.metadata.name,
          namespace: obj.metadata.namespace,
        },
        data: {
          test: 'updated',
        },
      }
  , cms
)`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cml corev1.ConfigMapList
			require.NoError(t, c.List(ctx, &cml, client.InNamespace(testns)))

			var cms []string
			for _, cm := range cml.Items {
				cms = append(cms, strings.Join([]string{cm.Name, cm.Data["test"]}, ":"))
			}
			assert.ElementsMatch(t, []string{"test3:updated"}, cms)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("deletion ignores NotFound errors", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				ApplyOptions: espejotev1alpha1.ApplyOptions{
					Force: true,
				},
				Template: `
local esp = import 'espejote.libsonnet';

[
  esp.markForDelete({
    apiVersion: "v1",
    kind: "ConfigMap",
    metadata: {
      name: "test",
      namespace: "` + testns + `",
    },
  }),
]
`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
			assert.Equal(t, "Ready", mr.Status.Status)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("strip managedFields", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		contextCM := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "context",
				Namespace: testns,
			},
			Data: map[string]string{
				"test": "test",
			},
		}
		require.NoError(t, c.Patch(ctx, contextCM, client.Apply, client.FieldOwner("test")))

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{{
					Name: "trigger",
					WatchResource: espejotev1alpha1.TriggerWatchResource{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						MatchNames: []string{"context"},
					},
				}},
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "context",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion: "v1",
						Kind:       "ConfigMap",
						MatchNames: []string{"context"},
					},
				}},
				Template: `
local esp = import 'espejote.libsonnet';

if esp.triggerName() == 'trigger' then {
  apiVersion: 'v1',
  kind: 'ConfigMap',
  metadata: {
    name: 'output',
  },
  data: {
    local tmf = std.get(esp.triggerData().resource.metadata, 'managedFields'),
    triggerManagedFields: if tmf != null then std.manifestJsonMinified(std.length(tmf)) else '0',
    assert std.isArray(esp.context().context) : 'Context should be available',
    assert std.length(esp.context().context) == 1 : 'Must have one config map in context',
    local cmf = std.get(esp.context().context[0].metadata, 'managedFields'),
    contextManagedFields: if cmf != null then std.manifestJsonMinified(std.length(cmf)) else '0',
  },
}
`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
			assert.Equal(t, "Ready", mr.Status.Status)

			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "output"}, &cm))
			assert.Equal(t, "0", cm.Data["triggerManagedFields"])
			assert.Equal(t, "0", cm.Data["contextManagedFields"])
		}, 5*time.Second, 100*time.Millisecond)

		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.Triggers[0].WatchResource.StripManagedFields = ptr.To(false)
		mr.Spec.Context[0].Resource.StripManagedFields = ptr.To(false)
		require.NoError(t, c.Update(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
			assert.Equal(t, "Ready", mr.Status.Status)

			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "output"}, &cm))
			assert.NotEqual(t, "0", cm.Data["triggerManagedFields"])
			assert.NotEqual(t, "0", cm.Data["contextManagedFields"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("custom metrics", func(t *testing.T) {
		t.Parallel()

		testns := testutil.TmpNamespace(t, c)

		for i := range 100 {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test" + strconv.Itoa(i),
					Namespace: testns,
				},
			}
			if i%2 == 0 {
				cm.Labels = map[string]string{
					"managed": "true",
				}
			}
			require.NoError(t, c.Create(ctx, cm))
		}

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{{
					Name: "matching-cms",
					WatchResource: espejotev1alpha1.TriggerWatchResource{
						Kind:       "ConfigMap",
						APIVersion: "v1",
						MatchNames: []string{"test1", "test2"},
					},
				}},
				Context: []espejotev1alpha1.ManagedResourceContext{
					{
						Name: "cms",
						Resource: espejotev1alpha1.ContextResource{
							APIVersion: "v1",
							Kind:       "ConfigMap",
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"managed": "true"},
							},
						},
					}, {
						Name: "all-cms",
						Resource: espejotev1alpha1.ContextResource{
							APIVersion: "v1",
							Kind:       "ConfigMap",
						},
					},
				},
				Template: `[]`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		inNsGatherer := filteringGatherer{
			gatherer: urlGatherer(fmt.Sprintf("http://localhost:%d/metrics", metricsPort)),
			filter: func(mf *dto.MetricFamily, m *dto.Metric) bool {
				if mf.GetName() == "espejote_reconciles_total" {
					// We know that the trigger triggers at least two reconciles, one for each object.
					// We still should cap it as there might be more than two reconciles because of cache wait or apply conflicts.
					if metricHasLabelPair("namespace", testns)(m) && metricHasLabelPair("trigger", "matching-cms")(m) {
						if m.GetCounter().GetValue() > 2 {
							m.Counter.Value = ptr.To(float64(2))
						}
						return true
					}
					return false
				}
				return metricHasLabelPair("namespace", testns)(m)
			},
		}

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			assert.NoError(t, promtestutil.GatherAndCompare(inNsGatherer, strings.NewReader(`
# HELP espejote_cached_objects Number of objects in the cache.
# TYPE espejote_cached_objects gauge
espejote_cached_objects{managedresource="test",name="all-cms",namespace="`+testns+`",type="context"} 100
espejote_cached_objects{managedresource="test",name="cms",namespace="`+testns+`",type="context"} 50
espejote_cached_objects{managedresource="test",name="matching-cms",namespace="`+testns+`",type="trigger"} 2
# HELP espejote_cache_size_bytes Size of the cache in bytes. Note that this is an approximation. The metric should not be compared across different espejote versions.
# TYPE espejote_cache_size_bytes gauge
espejote_cache_size_bytes{managedresource="test",name="all-cms",namespace="`+testns+`",type="context"} 67522
espejote_cache_size_bytes{managedresource="test",name="cms",namespace="`+testns+`",type="context"} 37077
espejote_cache_size_bytes{managedresource="test",name="matching-cms",namespace="`+testns+`",type="trigger"} 1674
# HELP espejote_reconciles_total Total number of reconciles by trigger.
# TYPE espejote_reconciles_total counter
espejote_reconciles_total{managedresource="test",namespace="`+testns+`",trigger="matching-cms"} 2
`), "espejote_cached_objects", "espejote_cache_size_bytes", "espejote_reconciles_total"), "espejote_cache_size_bytes may needs updating when switching between client or apiserver versions")
		}, 5*time.Second, 100*time.Millisecond)

		t.Log("error metrics")
		require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
		mr.Spec.Template = `glug`
		require.NoError(t, c.Update(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			metrics, err := inNsGatherer.Gather()
			require.NoError(t, err)
			filtered := filterMetrics(metrics, func(mf *dto.MetricFamily, m *dto.Metric) bool {
				return mf.GetName() == "espejote_reconcile_errors_total" &&
					metricHasLabelPair("trigger", "")(m) &&
					metricHasLabelPair("error_kind", string(TemplateError))(m)
			})
			assert.Len(t, filtered, 1, "expected one error metric with the error kind %q", TemplateError)
		}, 5*time.Second, 100*time.Millisecond)

		lintproblem, err := promtestutil.GatherAndLint(inNsGatherer)
		require.NoError(t, err)
		assert.Empty(t, lintproblem)
	})
}

func Test_ManagedResourceReconciler_Reconcile_WithBlockingCache(t *testing.T) {
	// Tests that the Resource stays in WaitingForCacheSync until the cache is ready.
	// The nice way™️ would be to add a slow/ blocking aggregate API server so we don't need
	// to inject anything into the controller. Most likely not worth the effort.

	t.Parallel()

	scheme, cfg := testutil.SetupEnvtestEnv(t)
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	ctx := log.IntoContext(t.Context(), testr.New(t))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Logger: testr.New(t),
	})
	require.NoError(t, err)
	cf, unblock := newSlowCachefunc()
	subject := &ManagedResourceControllerManager{
		Client:                  c,
		Scheme:                  c.Scheme(),
		ControllerLifetimeCtx:   ctx,
		JsonnetLibraryNamespace: "jsonnetlibs",
		Recorder:                mgr.GetEventRecorderFor("managed-resource-controller"),
		newCacheFunction:        cf,
	}
	require.NoError(t, subject.SetupWithManager("slow_client", cfg, mgr))

	mgrCtx, mgrCancel := context.WithCancel(ctx)
	t.Cleanup(mgrCancel)
	go func() {
		require.NoError(t, mgr.Start(mgrCtx))
	}()

	t.Run("test waiting for sync before becoming ready", func(t *testing.T) {
		testns := testutil.TmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{{
					Name: "context",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion: "v1",
						Kind:       "ConfigMap",
					},
				}},
				Template: `null`,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
			assert.Equal(t, "WaitingForCacheSync", mr.Status.Status)
		}, 5*time.Second, 10*time.Millisecond)

		t.Log("unblocking the cache sync")
		unblock()

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(mr), mr))
			assert.Equal(t, "Ready", mr.Status.Status)
		}, 5*time.Second, 10*time.Millisecond)

		var events corev1.EventList
		require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorForManagedResource(mr.Name)))
		require.Len(t, events.Items, 0, "waiting for caches should not create error events")
	})
}

func newSlowCachefunc() (cf func(*rest.Config, cache.Options) (cache.Cache, error), unblock func()) {
	block := make(chan struct{})
	return func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
			c, err := cache.New(config, opts)
			if err != nil {
				return nil, err
			}

			return &slowCache{
				Cache:     c,
				blockSync: block,
			}, nil
		}, func() {
			close(block)
		}
}

type slowCache struct {
	cache.Cache

	blockSync chan struct{}
}

func (sc *slowCache) WaitForCacheSync(ctx context.Context) bool {
	select {
	case <-ctx.Done():
	case <-sc.blockSync:
	}

	return sc.Cache.WaitForCacheSync(ctx)
}

// metricHasLabelPair returns a function that checks if a metric has a label pair with the given name and value.
func metricHasLabelPair(name, value string) func(*dto.Metric) bool {
	return func(m *dto.Metric) bool {
		return slices.ContainsFunc(m.Label, func(lp *dto.LabelPair) bool {
			return lp.GetName() == name && lp.GetValue() == value
		})
	}
}

// filterMetrics filters MetricFamilies based on a filter function.
// MetricFamilies are dropped if all their metrics are filtered.
func filterMetrics(mfs []*dto.MetricFamily, filter func(*dto.MetricFamily, *dto.Metric) (keep bool)) []*dto.MetricFamily {
	for _, mf := range mfs {
		mf.Metric = slices.DeleteFunc(mf.Metric, func(m *dto.Metric) bool { return !filter(mf, m) })
	}

	return slices.DeleteFunc(mfs, func(mf *dto.MetricFamily) bool { return len(mf.Metric) < 1 })
}

// filteringGatherer is a prometheus.Gatherer that filters metrics based on a filter function.
// MetricFamilies are dropped if all their metrics are filtered.
type filteringGatherer struct {
	gatherer prometheus.Gatherer
	filter   func(*dto.MetricFamily, *dto.Metric) (keep bool)
}

func (f filteringGatherer) Gather() ([]*dto.MetricFamily, error) {
	mfs, err := f.gatherer.Gather()
	return filterMetrics(mfs, f.filter), err
}

func urlGatherer(url string) prometheus.Gatherer {
	return prometheus.GathererFunc(func() ([]*dto.MetricFamily, error) {
		resp, err := http.Get(url)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, err := io.ReadAll(resp.Body)
			return nil, multierr.Combine(
				fmt.Errorf("unexpected status code %d, body: %q", resp.StatusCode, string(b)),
				err,
			)
		}

		dec := expfmt.NewDecoder(resp.Body, expfmt.NewFormat(expfmt.TypeTextPlain))

		metrics := make([]*dto.MetricFamily, 0)
		errs := make([]error, 0)
		for {
			mf := &dto.MetricFamily{}
			err := dec.Decode(mf)
			if err == io.EOF {
				break
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			metrics = append(metrics, mf)
		}

		return metrics, multierr.Combine(errs...)
	})
}

func eventSelectorFor(kind, name string) client.ListOption {
	return client.MatchingFieldsSelector{
		Selector: fields.AndSelectors(
			fields.OneTermEqualSelector("involvedObject.kind", kind),
			fields.OneTermEqualSelector("involvedObject.name", name),
		),
	}
}

func eventSelectorForManagedResource(managedResourceName string) client.ListOption {
	return eventSelectorFor("ManagedResource", managedResourceName)
}

// freePort returns a free port on the host.
func freePort() (int, error) {
	a, err := net.ResolveTCPAddr("tcp", ":0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
