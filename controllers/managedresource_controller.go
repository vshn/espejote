package controllers

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

type ManagedResourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=espejote.io,resources=managedresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=espejote.io,resources=managedresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=espejote.io,resources=managedresources/finalizers,verbs=update

func (r *ManagedResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceReconciler.Reconcile")
	l.Info("Reconciling ManagedResource")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&espejotev1alpha1.ManagedResource{}).
		Complete(r)
}
