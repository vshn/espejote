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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

// Test_ManagedResourceReconciler_Reconcile tests the ManagedResourceReconciler.
// For efficiency, the tests are run in parallel and there is only one instance of the controller and api-server.
// It is in the responsibility of the test to ensure that the resources do not conflict with each other.
// Tests can use the `tmpNamespace()“ function to create a new namespace that is guaranteed to not conflict with namespaces of other tests.
// Special care must be taken when modifying cluster scoped resources, for example by prefixing the resource names with the name returned from `tmpNamespace()`.
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
		localJsonnetLib := &espejotev1alpha1.JsonnetLibrary{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.JsonnetLibrarySpec{
				Data: map[string]string{
					"test.jsonnet": `{hello: "local-hello"}`,
				},
			},
		}
		require.NoError(t, c.Create(ctx, localJsonnetLib))

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
local test = import "lib/test/test.jsonnet";
local localTest = import "test/test.jsonnet";
local trigger = esp.getTrigger();

if esp.triggerType() == esp.TriggerTypeWatchResource && trigger.kind == "Namespace" then [{
	apiVersion: 'v1',
	kind: 'ConfigMap',
	metadata: {
		name: 'test',
		namespace: trigger.metadata.name,
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
			assert.Equal(t, "world", cm.Annotations["espejote.vshn.net/hello"])
			assert.Equal(t, "local-hello", cm.Annotations["espejote.vshn.net/local-hello"])
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("reconfigure filtered contexts", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, TemplateError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("duplicate context definition", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Context: []espejotev1alpha1.ManagedResourceContext{
					{Def: "test"},
					{Def: "test"},
				},
				Template: ``,
			},
		}
		require.NoError(t, c.Create(ctx, mr))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var events corev1.EventList
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, DependencyConfigurationError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("service account does not exist", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ServiceAccountError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("invalid template return", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, TemplateReturnError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("object with unknown api returned", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, ApplyError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("trigger api is not registered", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

		mr := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testns,
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
			require.Len(t, events.Items, 1)
			assert.Equal(t, "Warning", events.Items[0].Type)
			assert.Contains(t, events.Items[0].Message, DependencyConfigurationError)
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("force ownership", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
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

	t.Run("override field manager", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
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

	t.Run("field validation Ignore", func(t *testing.T) {
		t.Parallel()
		t.Log("field validation Ignore seems to have no effect on the apply process. This test is here to document this behavior and see if the behavior changes in the future.")

		testns := tmpNamespace(t, c)

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
			require.NoError(t, c.List(ctx, &events, client.InNamespace(testns), eventSelectorFor(mr.Name)))
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

	t.Run("interval trigger", func(t *testing.T) {
		t.Parallel()

		testns := tmpNamespace(t, c)

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

		require.Never(t, func() bool {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: testns, Name: "test"}, &cm))
			return cm.Data["test"] != "test"
		}, 2*time.Second, 10*time.Millisecond, "Trigger should be shut down and thus stop updating the resource")
	})
}

func eventSelectorFor(managedResourceName string) client.ListOption {
	return client.MatchingFieldsSelector{
		Selector: fields.AndSelectors(
			fields.OneTermEqualSelector("involvedObject.kind", "ManagedResource"),
			fields.OneTermEqualSelector("involvedObject.name", managedResourceName),
		),
	}
}

// tmpNamespace creates a new namespace, with default service account, with a generated name and registers a cleanup function to delete it.
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
	require.NoError(t, c.Create(t.Context(), ns))

	defaultSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns.Name,
		},
	}
	require.NoError(t, c.Create(t.Context(), defaultSA))

	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), ns))
	})
	return ns.Name
}
