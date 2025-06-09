package controllers

import (
	"context"
	"errors"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/plugins"
)

//+kubebuilder:rbac:groups=espejote.io,resources=plugins,verbs=get;list;watch
//+kubebuilder:rbac:groups=espejote.io,resources=plugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=espejote.io,resources=plugins/finalizers,verbs=update

type PluginReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Namespace is the namespace where the controller operates.
	// Currently plugins can only be created in the same namespace as the controller.
	Namespace string

	PluginManager *plugins.Manager
}

func (r *PluginReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("PluginReconciler.Reconcile")
	l.Info("Reconciling Plugin")

	if req.Namespace != r.Namespace {
		l.Info("Skipping reconciliation for plugin in different namespace", "namespace", req.Namespace)
		return ctrl.Result{}, nil
	}

	var plugin espejotev1alpha1.Plugin
	if err := r.Get(ctx, req.NamespacedName, &plugin); err != nil {
		if apierrors.IsNotFound(err) {
			// TODO de-register plugin from manager
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !plugin.DeletionTimestamp.IsZero() {
		// TODO de-register plugin from manager
		return ctrl.Result{}, nil
	}

	moduleRef := plugin.Spec.Module
	{
		pre, remain, found := strings.Cut(plugin.Spec.Module, "://")
		if found && pre != "registry" {
			l.Info("Invalid module format, expected 'registry://'", "module", plugin.Spec.Module)
			plugin.Status.Status = "InvalidModuleFormat"
			if err := r.Status().Update(ctx, &plugin); err != nil {
				l.Error(err, "Failed to update plugin status")
				return ctrl.Result{}, err
			}
			r.Recorder.Event(&plugin, "Warning", "InvalidModuleFormat", "Module format must start with 'registry://'")
			return ctrl.Result{}, nil
		} else if found {
			moduleRef = remain
		}
	}

	var status string
	if err := r.PluginManager.RegisterPlugin(ctx, plugin.Name, moduleRef); err != nil {
		if errors.Is(err, plugins.ErrAlreadyRegistered) {
			return ctrl.Result{}, nil // Plugin is already registered, nothing to do
		}
		l.Error(err, "Failed to register plugin")
		status = "Error"
		r.Recorder.Event(&plugin, "Warning", "PluginRegistrationFailed", err.Error())
	} else {
		status = "Registered"
		r.Recorder.Event(&plugin, "Normal", "PluginRegistered", "Plugin registered successfully")
	}
	plugin.Status.Status = status
	if err := r.Status().Update(ctx, &plugin); err != nil {
		l.Error(err, "Failed to update plugin status")
		return ctrl.Result{}, err
	}
	l.Info("Plugin reconciled", "plugin", plugin.Name, "status", status)
	return ctrl.Result{}, nil
}

// Setup sets up the controller with the Manager.
func (r *PluginReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return builder.ControllerManagedBy(mgr).
		For(&espejotev1alpha1.Plugin{}).
		Complete(r)
}
