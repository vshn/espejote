package controllers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/testutil"
)

func Test_AdmissionReconciler_Reconcile(t *testing.T) {
	const webhookName = "espejote-webhook"

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
	subject := &AdmissionReconciler{
		Client:                c,
		MutatingWebhookName:   webhookName,
		ValidatingWebhookName: webhookName,
		WebhookPort:           9443,
		WebhookServiceName:    webhookName,
		ControllerNamespace:   "system",
	}
	require.NoError(t, subject.SetupWithManager(mgr))

	mgrCtx, mgrCancel := context.WithCancel(ctx)
	t.Cleanup(mgrCancel)
	go func() {
		require.NoError(t, mgr.Start(mgrCtx))
	}()

	testns := testutil.TmpNamespace(t, c)

	val := espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "val",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Mutating: false,
			WebhookConfiguration: espejotev1alpha1.WebhookConfiguration{
				Rules: []admissionregistrationv1.RuleWithOperations{
					{
						Operations: []admissionregistrationv1.OperationType{
							admissionregistrationv1.Create,
						},
						Rule: admissionregistrationv1.Rule{
							APIGroups:   []string{"*"},
							APIVersions: []string{"*"},
							Resources:   []string{"*"},
						},
					},
				},
			},
		},
	}
	require.NoError(t, c.Create(ctx, &val))
	val2 := espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "zz-val",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Mutating:             false,
			WebhookConfiguration: *val.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(ctx, &val2))
	mut := espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mut",
			Namespace: testns,
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Mutating:             true,
			WebhookConfiguration: *val.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(ctx, &mut))

	var mutwebhook admissionregistrationv1.MutatingWebhookConfiguration
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &mutwebhook))
		require.Len(t, mutwebhook.Webhooks, 1)
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, strings.Join([]string{"mut", testns, "espejote.io"}, "."), mutwebhook.Webhooks[0].Name)
	assert.Equal(t, "system", mutwebhook.Webhooks[0].ClientConfig.Service.Namespace)
	assert.Equal(t, strings.Join([]string{"/dynamic", testns, "mut"}, "/"), *mutwebhook.Webhooks[0].ClientConfig.Service.Path)
	assert.Equal(t, ptr.To(int32(subject.WebhookPort)), mutwebhook.Webhooks[0].ClientConfig.Service.Port)
	assert.Equal(t, map[string]string{"kubernetes.io/metadata.name": testns}, mutwebhook.Webhooks[0].NamespaceSelector.MatchLabels)

	var valwebhook admissionregistrationv1.ValidatingWebhookConfiguration
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &valwebhook))
		require.Len(t, valwebhook.Webhooks, 2)
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, strings.Join([]string{"val", testns, "espejote.io"}, "."), valwebhook.Webhooks[0].Name)
	assert.Equal(t, "system", valwebhook.Webhooks[0].ClientConfig.Service.Namespace)
	assert.Equal(t, strings.Join([]string{"/dynamic", testns, "val"}, "/"), *valwebhook.Webhooks[0].ClientConfig.Service.Path)
	assert.Equal(t, ptr.To(int32(subject.WebhookPort)), valwebhook.Webhooks[0].ClientConfig.Service.Port)
	assert.Equal(t, map[string]string{"kubernetes.io/metadata.name": testns}, valwebhook.Webhooks[0].NamespaceSelector.MatchLabels)

	require.NoError(t, c.Delete(ctx, &val2))
	require.NoError(t, c.Delete(ctx, &mut))

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &valwebhook))
		require.Len(t, valwebhook.Webhooks, 1)
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &mutwebhook))
		require.Len(t, mutwebhook.Webhooks, 0)
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, c.Delete(ctx, &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookName,
		},
	}))

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &valwebhook))
		require.Len(t, valwebhook.Webhooks, 1)
	}, 5*time.Second, 10*time.Millisecond)
}
