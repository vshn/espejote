package controllers

import (
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"oras.land/oras-go/v2/registry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/plugins"
	"github.com/vshn/espejote/plugins/plugintest"
	"github.com/vshn/espejote/testutil"
)

func Test_PluginReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	ctx := log.IntoContext(t.Context(), testr.New(t))

	scheme, cfg := testutil.SetupEnvtestEnv(t)
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	testns := testutil.TmpNamespace(t, c)

	mockPluginRegistry := plugintest.NewMockPluginRegistry(t)
	require.NoError(t, err)
	pluginManager, err := plugins.NewManagerWithRegistry(t.TempDir(), mockPluginRegistry)
	require.NoError(t, err)

	echoPluginRef := registry.Reference{Registry: "plugins.espejote.net", Repository: "echo", Reference: "v1.0.0"}
	require.NoError(t, mockPluginRegistry.UploadFile("testdata/plugin-echo/plugin.wasm", echoPluginRef))

	existing := espejotev1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "echo",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.PluginSpec{
			Module: echoPluginRef.String(),
		},
	}
	require.NoError(t, c.Create(ctx, &existing))

	existingWithPrefix := espejotev1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "echo-with-prefix",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.PluginSpec{
			Module: "registry://" + echoPluginRef.String(),
		},
	}
	require.NoError(t, c.Create(ctx, &existingWithPrefix))

	wrongPrefix := espejotev1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wrong-prefix",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.PluginSpec{
			Module: "file://" + echoPluginRef.String(),
		},
	}
	require.NoError(t, c.Create(ctx, &wrongPrefix))

	missing := espejotev1alpha1.Plugin{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.PluginSpec{
			Module: "plugins.espejote.net/missing:v1.0.0",
		},
	}
	require.NoError(t, c.Create(ctx, &missing))

	recorder := record.NewFakeRecorder(10)
	subject := &PluginReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: recorder,

		Namespace: testns,

		PluginManager: pluginManager,
	}

	_, err = subject.Reconcile(ctx, requestFromObject(&existing))
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(&existing), &existing))
	assert.Equal(t, "Registered", existing.Status.Status)
	if assert.Len(t, recorder.Events, 1) {
		event := <-recorder.Events
		assert.Contains(t, event, "PluginRegistered")
	}

	_, err = subject.Reconcile(ctx, requestFromObject(&existingWithPrefix))
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(&existingWithPrefix), &existingWithPrefix))
	assert.Equal(t, "Registered", existingWithPrefix.Status.Status)
	if assert.Len(t, recorder.Events, 1) {
		event := <-recorder.Events
		assert.Contains(t, event, "PluginRegistered")
	}

	_, err = subject.Reconcile(ctx, requestFromObject(&wrongPrefix))
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(&wrongPrefix), &wrongPrefix))
	assert.Equal(t, "InvalidModuleFormat", wrongPrefix.Status.Status)
	if assert.Len(t, recorder.Events, 1) {
		event := <-recorder.Events
		assert.Contains(t, event, "InvalidModuleFormat")
		assert.Contains(t, event, "Module format must start with 'registry://'")
	}

	_, err = subject.Reconcile(ctx, requestFromObject(&missing))
	require.NoError(t, err)
	require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(&missing), &missing))
	assert.Contains(t, missing.Status.Status, "Error")
	if assert.Len(t, recorder.Events, 1) {
		event := <-recorder.Events
		assert.Contains(t, event, "PluginRegistrationFailed")
		assert.Contains(t, event, "not found")
	}
}

func requestFromObject(obj client.Object) ctrl.Request {
	return ctrl.Request{
		NamespacedName: client.ObjectKeyFromObject(obj),
	}
}
