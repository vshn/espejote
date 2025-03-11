package controllers

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

func Test_ManagedResourceReconciler_Reconcile(t *testing.T) {
	log.SetLogger(testr.New(t))
	scheme, cfg := setupEnvtestEnv(t)
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	ctx := log.IntoContext(t.Context(), testr.New(t))

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)
	subject := &ManagedResourceReconciler{
		Client:                  c,
		Scheme:                  c.Scheme(),
		ControllerLifetimeCtx:   ctx,
		JsonnetLibraryNamespace: "jsonnetlibs",
		Recorder:                mgr.GetEventRecorderFor("managed-resource-controller"),
	}
	require.NoError(t, subject.Setup(cfg, mgr))

	mgrCtx, mgrCancel := context.WithCancel(ctx)
	t.Cleanup(mgrCancel)
	go mgr.Start(mgrCtx)

	t.Run("reconcile from added watch resource trigger", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
					"test.jsonnet": `{hello: "world"}`,
				},
			},
		}
		require.NoError(t, c.Create(ctx, jsonnetLib))

		saForManagedResource := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: testns,
			},
		}
		require.NoError(t, c.Create(ctx, saForManagedResource))

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						WatchResource: espejotev1alpha1.TriggerWatchResource{
							Kind:       "Namespace",
							APIVersion: "v1",
							Name:       testns + "-2",
						},
					},
				},
				Template: `
local esp = import "espejote.libsonnet";
local test = import "test/test.jsonnet";
local trigger = esp.getTrigger();

if esp.triggerKnown() && trigger.kind == "Namespace" then [{
	apiVersion: 'v1',
	kind: 'ConfigMap',
	metadata: {
		name: 'test',
		namespace: trigger.metadata.name,
		annotations: {
			"espejote.vshn.net/hello": test.hello,
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
			assert.Equal(t, "world", cm.Annotations["espejote.vshn.net/hello"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("reconfigure filtered contexts", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

		saForManagedResource := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: testns,
			},
		}
		require.NoError(t, c.Create(ctx, saForManagedResource))

		for i := 0; i < 500; i++ {
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
					Def: "cms",
					Resource: espejotev1alpha1.ContextResource{
						APIVersion:  "v1",
						Kind:        "ConfigMap",
						IgnoreNames: []string{"collected", "test1", "test3"},
					},
				}},
				Template: `
local esp = import "espejote.libsonnet";
local cms = esp.getContext("cms");

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
	})

	t.Run("template error", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

		saForManagedResource := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "default",
				Namespace: testns,
			},
		}
		require.NoError(t, c.Create(ctx, saForManagedResource))

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Template: `glug`,
			},
		}
		require.NoError(t, c.Create(ctx, res))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), client.MatchingFieldsSelector{
				Selector: fields.AndSelectors(
					fields.OneTermEqualSelector("involvedObject.kind", "ManagedResource"),
					fields.OneTermEqualSelector("involvedObject.name", res.Name),
				),
			}))
			assert.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, TemplateError)
		}, 5*time.Second, 100*time.Millisecond)
	})
}

func tmpNamespace(t *testing.T, c client.Client) string {
	t.Helper()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "espejote-test-",
			Annotations: map[string]string{
				"test.espejote.vshn.net/name": t.Name(),
			},
		},
	}
	require.NoError(t, c.Create(context.Background(), ns))
	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), ns))
	})
	return ns.Name
}
