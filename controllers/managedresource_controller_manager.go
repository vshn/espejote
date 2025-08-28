package controllers

import (
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

type ManagedResourceControllerManager struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	ControllerLifetimeCtx   context.Context
	JsonnetLibraryNamespace string

	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	mapper     meta.RESTMapper
	logger     logr.Logger

	// newCacheFunc is only used for testing
	newCacheFunction cache.NewCacheFunc

	// controllers holds the actual reconcilers, one for each managed resource.
	controllersMux sync.RWMutex
	controllers    map[types.NamespacedName]*resourceController
}

type resourceController struct {
	ctrl       controller.TypedController[Request]
	reconciler *ManagedResourceReconciler
	in         chan event.TypedGenericEvent[Request]
	stop       func()
}

//+kubebuilder:rbac:groups=espejote.io,resources=managedresources,verbs=get;list;watch

func (r *ManagedResourceControllerManager) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceControllerManager.Reconcile")
	l.Info("Reconciling ManagedResource")

	var managedResource espejotev1alpha1.ManagedResource
	if err := r.Get(ctx, req.NamespacedName, &managedResource); err != nil {
		if apierrors.IsNotFound(err) {
			l.Info("ManagedResource is no longer available, stopping cache")
			r.stopAndRemoveControllerFor(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !managedResource.DeletionTimestamp.IsZero() {
		l.Info("ManagedResource is being deleted, stopping cache")
		r.stopAndRemoveControllerFor(req.NamespacedName)
		return ctrl.Result{}, nil
	}

	{
		ic, err := r.ensureInstanceControllerFor(req.NamespacedName)
		if err != nil {
			return ctrl.Result{}, newEspejoteError(err, ControllerInstantiationError)
		}
		ic.in <- event.TypedGenericEvent[Request]{Object: Request{}}
	}

	return ctrl.Result{}, nil
}

func (r *ManagedResourceControllerManager) ensureInstanceControllerFor(mrKey types.NamespacedName) (*resourceController, error) {
	r.controllersMux.RLock()
	if c := r.controllers[mrKey]; c != nil {
		r.controllersMux.RUnlock()
		return c, nil
	}
	r.controllersMux.RUnlock()

	r.controllersMux.Lock()
	defer r.controllersMux.Unlock()
	if c := r.controllers[mrKey]; c != nil {
		return c, nil
	}

	reconciler := &ManagedResourceReconciler{
		For: mrKey,

		Client:   r.Client,
		Scheme:   r.Scheme,
		Recorder: r.Recorder,

		ControllerLifetimeCtx:   r.ControllerLifetimeCtx,
		JsonnetLibraryNamespace: r.JsonnetLibraryNamespace,

		clientset:  r.clientset,
		restConfig: r.restConfig,
		mapper:     r.mapper,

		newCacheFunction: r.newCacheFunction,
	}
	dynCtrl, err := controller.NewTypedUnmanaged(
		fmt.Sprintf("managedresource/%s/%s", mrKey.Namespace, mrKey.Name),
		controller.TypedOptions[Request]{
			// The name is used for the workqueue metrics. Thus would trigger on
			// resource recreation. We could encode the resource UID into the metrics
			// to fix this nicely. This is quite annoying because you need to override
			// the new workqueue function. Don't currently see the problem with just
			// reusing the same metrics on recreate.
			SkipNameValidation: ptr.To(true),
			Reconciler:         reconciler,
			Logger:             r.logger,
		},
	)
	// Reconciler needs a backreference for additional dynamic watches from triggers.
	reconciler.controller = dynCtrl
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic controller: %w", err)
	}
	instanceCtrlCtx, instanceCtrlCancel := context.WithCancel(r.ControllerLifetimeCtx)
	instanceCtrl := &resourceController{
		in:         make(chan event.TypedGenericEvent[Request]),
		ctrl:       dynCtrl,
		stop:       instanceCtrlCancel,
		reconciler: reconciler,
	}

	if err := dynCtrl.Watch(source.TypedChannel(instanceCtrl.in, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, event Request) []Request {
		return []Request{event}
	}))); err != nil {
		return nil, fmt.Errorf("failed to watch instance %q controller: %w", mrKey, err)
	}
	go dynCtrl.Start(instanceCtrlCtx)
	if r.controllers == nil {
		r.controllers = make(map[types.NamespacedName]*resourceController)
	}
	r.controllers[mrKey] = instanceCtrl
	return instanceCtrl, nil
}

func (r *ManagedResourceControllerManager) stopAndRemoveControllerFor(mrKey types.NamespacedName) error {
	r.controllersMux.RLock()
	_, ok := r.controllers[mrKey]
	if !ok {
		r.controllersMux.RUnlock()
		return nil
	}
	r.controllersMux.RUnlock()

	r.controllersMux.Lock()
	defer r.controllersMux.Unlock()

	rc, ok := r.controllers[mrKey]
	if !ok {
		return nil
	}
	rc.stop()
	delete(r.controllers, mrKey)
	return nil
}

func (r *ManagedResourceControllerManager) SetupWithManager(name string, cfg *rest.Config, mgr ctrl.Manager) error {
	err := builder.ControllerManagedBy(mgr).
		For(&espejotev1alpha1.ManagedResource{}).
		Named(name).
		Complete(r)
	if err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	kubernetesClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	r.clientset = kubernetesClient
	r.restConfig = cfg
	r.mapper = mgr.GetRESTMapper()
	r.logger = mgr.GetLogger()

	return err
}
