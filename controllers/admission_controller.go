package controllers

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

// AdmissionReconciler reconciles Admission objects.
type AdmissionReconciler struct {
	client.Client

	MutatingWebhookName   string
	ValidatingWebhookName string

	WebhookPort         int
	WebhookServiceName  string
	ControllerNamespace string
}

//+kubebuilder:rbac:groups=espejote.io,resources=admissions,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=espejote.io,resources=admissions/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=espejote.io,resources=admissions/finalizers,verbs=update

//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

// Reconcile adds admissions to the MutatingWebhookConfiguration and ValidatingWebhookConfigurations.
func (r *AdmissionReconciler) Reconcile(ctx context.Context, _ reconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("AdmissionReconciler.reconcile")
	l.Info("Reconciling Admission")

	var admissions espejotev1alpha1.AdmissionList
	if err := r.List(ctx, &admissions); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list admissions: %w", err)
	}

	var validatingAdmissions []espejotev1alpha1.Admission
	var mutatingAdmissions []espejotev1alpha1.Admission

	for _, admission := range admissions.Items {
		if admission.Spec.Mutating {
			mutatingAdmissions = append(mutatingAdmissions, admission)
		} else {
			validatingAdmissions = append(validatingAdmissions, admission)
		}
	}
	slices.SortFunc(validatingAdmissions, func(a, b espejotev1alpha1.Admission) int {
		return strings.Compare(a.GetName(), b.GetName())
	})
	slices.SortFunc(mutatingAdmissions, func(a, b espejotev1alpha1.Admission) int {
		return strings.Compare(a.GetName(), b.GetName())
	})

	mutwebhook := &admissionregistrationv1.MutatingWebhookConfiguration{}
	mutwebhook.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("MutatingWebhookConfiguration"))
	mutwebhook.Name = r.MutatingWebhookName
	mutwebhook.Webhooks = make([]admissionregistrationv1.MutatingWebhook, 0, len(mutatingAdmissions))
	for _, admission := range mutatingAdmissions {
		mutwebhook.Webhooks = append(mutwebhook.Webhooks, admissionregistrationv1.MutatingWebhook{
			Name: strings.Join([]string{admission.Name, admission.Namespace, "espejote.io"}, "."),

			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Name:      r.WebhookServiceName,
					Namespace: r.ControllerNamespace,
					Path:      ptr.To(path.Join("/dynamic", admission.Namespace, admission.Name)),
					Port:      ptr.To(int32(r.WebhookPort)),
				},
			},

			Rules:                   admission.Spec.WebhookConfiguration.Rules,
			FailurePolicy:           admission.Spec.WebhookConfiguration.FailurePolicy,
			MatchPolicy:             admission.Spec.WebhookConfiguration.MatchPolicy,
			NamespaceSelector:       admission.Spec.WebhookConfiguration.NamespaceSelector,
			ObjectSelector:          admission.Spec.WebhookConfiguration.ObjectSelector,
			SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
			AdmissionReviewVersions: []string{"v1"},
			ReinvocationPolicy:      admission.Spec.WebhookConfiguration.ReinvocationPolicy,
			MatchConditions:         admission.Spec.WebhookConfiguration.MatchConditions,
		})
	}
	if err := r.Client.Patch(ctx, mutwebhook, client.Apply, client.FieldOwner("espejote-webhook-controller")); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch MutatingWebhookConfiguration: %w", err)
	}

	valwebhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	valwebhook.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("ValidatingWebhookConfiguration"))
	valwebhook.Name = r.MutatingWebhookName
	valwebhook.Webhooks = make([]admissionregistrationv1.ValidatingWebhook, 0, len(validatingAdmissions))
	for _, admission := range validatingAdmissions {
		valwebhook.Webhooks = append(valwebhook.Webhooks, admissionregistrationv1.ValidatingWebhook{
			Name: strings.Join([]string{admission.Name, admission.Namespace, "espejote.io"}, "."),

			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Name:      r.WebhookServiceName,
					Namespace: r.ControllerNamespace,
					Path:      ptr.To(path.Join("/dynamic", admission.Namespace, admission.Name)),
					Port:      ptr.To(int32(r.WebhookPort)),
				},
			},

			Rules:                   admission.Spec.WebhookConfiguration.Rules,
			FailurePolicy:           admission.Spec.WebhookConfiguration.FailurePolicy,
			MatchPolicy:             admission.Spec.WebhookConfiguration.MatchPolicy,
			NamespaceSelector:       admission.Spec.WebhookConfiguration.NamespaceSelector,
			ObjectSelector:          admission.Spec.WebhookConfiguration.ObjectSelector,
			SideEffects:             ptr.To(admissionregistrationv1.SideEffectClassNone),
			AdmissionReviewVersions: []string{"v1"},
			MatchConditions:         admission.Spec.WebhookConfiguration.MatchConditions,
		})
	}
	if err := r.Client.Patch(ctx, valwebhook, client.Apply, client.FieldOwner("espejote-webhook-controller")); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch MutatingWebhookConfiguration: %w", err)
	}

	return ctrl.Result{}, nil
}

// Setup sets up the controller with the Manager.
func (r *AdmissionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return builder.ControllerManagedBy(mgr).
		For(&espejotev1alpha1.Admission{}).
		Watches(&admissionregistrationv1.MutatingWebhookConfiguration{}, handler.EnqueueRequestsFromMapFunc(mapToAllAdmissions(mgr.GetClient()))).
		Watches(&admissionregistrationv1.ValidatingWebhookConfiguration{}, handler.EnqueueRequestsFromMapFunc(mapToAllAdmissions(mgr.GetClient()))).
		Complete(r)
}

func mapToAllAdmissions(c client.Reader) func(_ context.Context, _ client.Object) []reconcile.Request {
	return func(_ context.Context, _ client.Object) []reconcile.Request {
		var admissions espejotev1alpha1.AdmissionList
		if err := c.List(context.Background(), &admissions); err != nil {
			log.FromContext(context.Background()).Error(err, "Failed to list Admissions")
			return nil
		}
		reqs := make([]reconcile.Request, len(admissions.Items))
		for i := range admissions.Items {
			reqs[i].NamespacedName = types.NamespacedName{
				Namespace: admissions.Items[i].Namespace,
				Name:      admissions.Items[i].Name,
			}
		}
		return reqs
	}
}
