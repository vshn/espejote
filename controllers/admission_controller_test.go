package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/testutil"
)

func Test_AdmissionReconciler_Reconcile(t *testing.T) {
	t.Parallel()

	scheme, cfg := testutil.SetupEnvtestEnv(t)
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	val := espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "val",
			Namespace: "default",
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
	require.NoError(t, c.Create(context.Background(), &val))
	mut := espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mut",
			Namespace: "default",
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Mutating:             true,
			WebhookConfiguration: *val.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(context.Background(), &mut))

	subject := &AdmissionReconciler{
		Client:                c,
		MutatingWebhookName:   "espejote-webhook",
		ValidatingWebhookName: "espejote-webhook",
		WebhookPort:           9443,
		WebhookServiceName:    "espejote-webhook",
		ControllerNamespace:   "system",
	}

	_, err = subject.Reconcile(context.Background(), reconcile.Request{})
	require.NoError(t, err)

	var mutwebhook admissionregistrationv1.MutatingWebhookConfiguration
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "espejote-webhook"}, &mutwebhook))
	require.Len(t, mutwebhook.Webhooks, 1)
	assert.Equal(t, "mut.default.espejote.io", mutwebhook.Webhooks[0].Name)
	assert.Equal(t, "system", mutwebhook.Webhooks[0].ClientConfig.Service.Namespace)
	assert.Equal(t, "/dynamic/default/mut", *mutwebhook.Webhooks[0].ClientConfig.Service.Path)
	assert.Equal(t, ptr.To(int32(subject.WebhookPort)), mutwebhook.Webhooks[0].ClientConfig.Service.Port)

	var valwebhook admissionregistrationv1.ValidatingWebhookConfiguration
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Name: "espejote-webhook"}, &valwebhook))
	require.Len(t, valwebhook.Webhooks, 1)
	assert.Equal(t, "val.default.espejote.io", valwebhook.Webhooks[0].Name)
	assert.Equal(t, "system", valwebhook.Webhooks[0].ClientConfig.Service.Namespace)
	assert.Equal(t, "/dynamic/default/val", *valwebhook.Webhooks[0].ClientConfig.Service.Path)
	assert.Equal(t, ptr.To(int32(subject.WebhookPort)), valwebhook.Webhooks[0].ClientConfig.Service.Port)
}
