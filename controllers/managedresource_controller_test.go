package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	t.Run("reconcile from added watch resource trigger", func(t *testing.T) {
		ctx := log.IntoContext(t.Context(), testr.New(t))

		subject := &ManagedResourceReconciler{
			Client:                c,
			Scheme:                c.Scheme(),
			ControllerLifetimeCtx: ctx,
			RESTConfig:            cfg,

			JsonnetLibraryNamespace: "jsonnetlibs",
		}
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme,
		})
		require.NoError(t, err)
		require.NoError(t, subject.SetupWithManager(mgr))

		mgrCtx, mgrCancel := context.WithCancel(ctx)
		t.Cleanup(mgrCancel)
		go mgr.Start(mgrCtx)

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

		res := &espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: espejotev1alpha1.ManagedResourceSpec{
				Triggers: []espejotev1alpha1.ManagedResourceTrigger{
					{
						WatchResource: espejotev1alpha1.TriggerWatchResource{
							Kind:       "Namespace",
							APIVersion: "v1",
						},
					},
				},
				Template: `
local trigger = std.extVar("trigger");
local test = import "test/test.jsonnet";

if trigger != null && trigger.kind == "Namespace" then [{
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
				Name: "test",
			},
		}
		require.NoError(t, c.Create(ctx, ns))

		require.EventuallyWithT(t, func(t *assert.CollectT) {
			var cm corev1.ConfigMap
			require.NoError(t, c.Get(ctx, types.NamespacedName{Namespace: "test", Name: "test"}, &cm))
			assert.Equal(t, "world", cm.Annotations["espejote.vshn.net/hello"])
		}, 5*time.Second, 100*time.Millisecond)
	})
}
