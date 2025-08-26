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

	expControllersMux sync.RWMutex
	expControllers    map[types.NamespacedName]*resourceController
}

type resourceController struct {
	ctrl controller.TypedController[Request]
	in   chan event.TypedGenericEvent[Request]
	stop func()
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
		ic.in <- event.TypedGenericEvent[Request]{Object: Request{NamespacedName: req.NamespacedName}}
	}

	return ctrl.Result{}, nil
}

func (r *ManagedResourceControllerManager) ensureInstanceControllerFor(mrKey types.NamespacedName) (*resourceController, error) {
	r.expControllersMux.RLock()
	if c := r.expControllers[mrKey]; c != nil {
		r.expControllersMux.RUnlock()
		return c, nil
	}
	r.expControllersMux.RUnlock()

	r.expControllersMux.Lock()
	defer r.expControllersMux.Unlock()
	if c := r.expControllers[mrKey]; c != nil {
		return c, nil
	}

	reconciler := &ManagedResourceReconciler{
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
		fmt.Sprintf("mr-dynamic-%s-%s", mrKey.Namespace, mrKey.Name),
		controller.TypedOptions[Request]{
			SkipNameValidation: ptr.To(true),
			Reconciler:         reconciler,
			Logger:             r.logger,
		},
	)
	reconciler.controller = dynCtrl
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic controller: %w", err)
	}
	instanceCtrlCtx, instanceCtrlCancel := context.WithCancel(r.ControllerLifetimeCtx)
	instanceCtrl := &resourceController{
		in:   make(chan event.TypedGenericEvent[Request]),
		ctrl: dynCtrl,
		stop: instanceCtrlCancel,
	}

	if err := dynCtrl.Watch(source.TypedChannel(instanceCtrl.in, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, event Request) []Request {
		return []Request{event}
	}))); err != nil {
		return nil, fmt.Errorf("failed to watch instance %q controller: %w", mrKey, err)
	}
	go dynCtrl.Start(instanceCtrlCtx)
	if r.expControllers == nil {
		r.expControllers = make(map[types.NamespacedName]*resourceController)
	}
	r.expControllers[mrKey] = instanceCtrl
	return instanceCtrl, nil
}

func (r *ManagedResourceControllerManager) stopAndRemoveControllerFor(mrKey types.NamespacedName) error {
	r.expControllersMux.RLock()
	_, ok := r.expControllers[mrKey]
	if !ok {
		r.expControllersMux.RUnlock()
		return nil
	}
	r.expControllersMux.RUnlock()

	r.expControllersMux.Lock()
	defer r.expControllersMux.Unlock()

	rc, ok := r.expControllers[mrKey]
	if !ok {
		return nil
	}
	rc.stop()
	delete(r.expControllers, mrKey)
	return nil
}

func (r *ManagedResourceControllerManager) SetupWithManager(cfg *rest.Config, mgr ctrl.Manager) error {
	err := builder.ControllerManagedBy(mgr).
		For(&espejotev1alpha1.ManagedResource{}).
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
