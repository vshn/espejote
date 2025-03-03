package controllers

import (
	"context"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type ClusterManagedResourceReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=espejote.io,resources=clustermanagedresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=espejote.io,resources=clustermanagedresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=espejote.io,resources=clustermanagedresources/finalizers,verbs=update

func (r *ClusterManagedResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ClusterManagedResourceReconciler.Reconcile")
	l.Info("Reconciling ClusterManagedResource")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterManagedResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&espejotev1alpha1.ClusterManagedResource{}).
		Complete(r)
}
