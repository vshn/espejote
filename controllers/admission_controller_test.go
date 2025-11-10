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
	corev1 "k8s.io/api/core/v1"
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
							Scope:       ptr.To(admissionregistrationv1.AllScopes),
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

	require.NoError(t, c.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "zz-" + testns,
		},
	}))
	mut2 := espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mut",
			Namespace: "zz-" + testns,
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Mutating:             true,
			WebhookConfiguration: *val.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(ctx, &mut2))

	clusterVal := espejotev1alpha1.ClusterAdmission{
		ObjectMeta: metav1.ObjectMeta{
			Name: "val-" + testns,
		},
		Spec: espejotev1alpha1.ClusterAdmissionSpec{
			Mutating: false,
			WebhookConfiguration: espejotev1alpha1.WebhookConfigurationWithNamespaceSelector{
				NamespaceSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "runlevel",
							Operator: metav1.LabelSelectorOpNotIn,
							Values:   []string{"0", "1"},
						},
					},
				},
				WebhookConfiguration: *val.Spec.WebhookConfiguration.DeepCopy(),
			},
		},
	}
	require.NoError(t, c.Create(ctx, &clusterVal))

	clusterVal2 := espejotev1alpha1.ClusterAdmission{
		ObjectMeta: metav1.ObjectMeta{
			Name: "zz-val-" + testns,
		},
		Spec: espejotev1alpha1.ClusterAdmissionSpec{
			Mutating:             false,
			WebhookConfiguration: *clusterVal.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(ctx, &clusterVal2))

	clusterMut := espejotev1alpha1.ClusterAdmission{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mut-" + testns,
		},
		Spec: espejotev1alpha1.ClusterAdmissionSpec{
			Mutating:             true,
			WebhookConfiguration: *clusterVal.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(ctx, &clusterMut))

	clusterMut2 := espejotev1alpha1.ClusterAdmission{
		ObjectMeta: metav1.ObjectMeta{
			Name: "zz-mut-" + testns,
		},
		Spec: espejotev1alpha1.ClusterAdmissionSpec{
			Mutating:             true,
			WebhookConfiguration: *clusterVal.Spec.WebhookConfiguration.DeepCopy(),
		},
	}
	require.NoError(t, c.Create(ctx, &clusterMut2))

	var mutwebhook admissionregistrationv1.MutatingWebhookConfiguration
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &mutwebhook))
		require.Len(t, mutwebhook.Webhooks, 4)
	}, 5*time.Second, 10*time.Millisecond)
	assertWebhookNames(t, mutwebhook, []string{
		strings.Join([]string{mut.Name, mut.Namespace, "namespaced.espejote.io"}, "."),
		strings.Join([]string{mut2.Name, mut2.Namespace, "namespaced.espejote.io"}, "."),
		strings.Join([]string{clusterMut.Name, "cluster.espejote.io"}, "."),
		strings.Join([]string{clusterMut2.Name, "cluster.espejote.io"}, "."),
	})
	{
		c := mutwebhook.Webhooks[0]
		assert.Equal(t, "system", c.ClientConfig.Service.Namespace)
		assert.Equal(t, strings.Join([]string{"/dynamic", mut.Namespace, mut.Name}, "/"), *c.ClientConfig.Service.Path)
		assert.Equal(t, ptr.To(int32(subject.WebhookPort)), c.ClientConfig.Service.Port)
		assert.Equal(t, map[string]string{"kubernetes.io/metadata.name": testns}, c.NamespaceSelector.MatchLabels)
		if assert.Len(t, c.Rules, 1) {
			assert.Equal(t, ptr.To(admissionregistrationv1.NamespacedScope), c.Rules[0].Scope, "scope should be Namespaced for namespaced admission")
		}
	}
	{
		c := mutwebhook.Webhooks[2]
		assert.Equal(t, "system", c.ClientConfig.Service.Namespace)
		assert.Equal(t, strings.Join([]string{"/dynamic-cluster", clusterMut.Name}, "/"), *c.ClientConfig.Service.Path)
		assert.Equal(t, ptr.To(int32(subject.WebhookPort)), c.ClientConfig.Service.Port)
		assert.Equal(t, &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "runlevel",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"0", "1"},
				},
			},
		}, c.NamespaceSelector)
		if assert.Len(t, c.Rules, 1) {
			assert.Equal(t, ptr.To(admissionregistrationv1.AllScopes), c.Rules[0].Scope)
		}
	}

	var valwebhook admissionregistrationv1.ValidatingWebhookConfiguration
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &valwebhook))
		require.Len(t, valwebhook.Webhooks, 4)
	}, 5*time.Second, 10*time.Millisecond)
	assertWebhookNames(t, valwebhook, []string{
		strings.Join([]string{val.Name, val.Namespace, "namespaced.espejote.io"}, "."),
		strings.Join([]string{val2.Name, val2.Namespace, "namespaced.espejote.io"}, "."),
		strings.Join([]string{clusterVal.Name, "cluster.espejote.io"}, "."),
		strings.Join([]string{clusterVal2.Name, "cluster.espejote.io"}, "."),
	})
	{
		c := valwebhook.Webhooks[0]
		assert.Equal(t, "system", c.ClientConfig.Service.Namespace)
		assert.Equal(t, strings.Join([]string{"/dynamic", val.Namespace, val.Name}, "/"), *c.ClientConfig.Service.Path)
		assert.Equal(t, ptr.To(int32(subject.WebhookPort)), c.ClientConfig.Service.Port)
		assert.Equal(t, map[string]string{"kubernetes.io/metadata.name": testns}, c.NamespaceSelector.MatchLabels)
		if assert.Len(t, c.Rules, 1) {
			assert.Equal(t, ptr.To(admissionregistrationv1.NamespacedScope), c.Rules[0].Scope, "scope should be Namespaced for namespaced admission")
		}
	}
	{
		c := valwebhook.Webhooks[2]
		assert.Equal(t, "system", c.ClientConfig.Service.Namespace)
		assert.Equal(t, strings.Join([]string{"/dynamic-cluster", clusterVal.Name}, "/"), *c.ClientConfig.Service.Path)
		assert.Equal(t, ptr.To(int32(subject.WebhookPort)), c.ClientConfig.Service.Port)
		assert.Equal(t, &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "runlevel",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"0", "1"},
				},
			},
		}, c.NamespaceSelector)
		if assert.Len(t, c.Rules, 1) {
			assert.Equal(t, ptr.To(admissionregistrationv1.AllScopes), c.Rules[0].Scope)
		}
	}

	for _, o := range []client.Object{&val2, &clusterVal2, &mut, &mut2, &clusterMut, &clusterMut2} {
		require.NoError(t, c.Delete(ctx, o))
	}

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: webhookName}, &valwebhook))
		require.Len(t, valwebhook.Webhooks, 2)
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
		require.Len(t, valwebhook.Webhooks, 2)
	}, 5*time.Second, 10*time.Millisecond)
}

// assertWebhookNames asserts that the given webhook (mutating or validating) has the expected webhook names in correct order.
func assertWebhookNames(t *testing.T, webhook any, expectedNames []string, msgAndArgs ...any) {
	switch w := webhook.(type) {
	case admissionregistrationv1.MutatingWebhookConfiguration:
		names := make([]string, 0, len(w.Webhooks))
		for _, wh := range w.Webhooks {
			names = append(names, wh.Name)
		}
		assert.Equal(t, expectedNames, names, msgAndArgs...)
	case admissionregistrationv1.ValidatingWebhookConfiguration:
		names := make([]string, 0, len(w.Webhooks))
		for _, wh := range w.Webhooks {
			names = append(names, wh.Name)
		}
		assert.Equal(t, expectedNames, names, msgAndArgs...)
	default:
		t.Fatalf("unsupported webhook type: %T", webhook)
	}
}
