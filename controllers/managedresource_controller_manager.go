package controllers

import (
	"context"
	"crypto/md5"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/multierr"
	authv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
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
	Recorder events.EventRecorder

	ControllerLifetimeCtx   context.Context
	JsonnetLibraryNamespace string

	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	mapper     meta.RESTMapper
	logger     logr.Logger

	// notifyChan is used to notify the manager of async events such as controller start failures.
	notifyChan chan event.TypedGenericEvent[ctrl.Request]

	// controllersMux protects the controllers map.
	// While this controller does not access the map concurrently the metrics collector does.
	controllersMux sync.RWMutex
	// controllers holds the actual reconcilers, one for each managed resource.
	controllers map[types.NamespacedName]*resourceController
}

// ErrStopped is returned by StartErr() if the controller was stopped without error.
var ErrStopped = errors.New("controller stopped")

type resourceController struct {
	configHash string

	ctrl       controller.TypedController[Request]
	reconciler *ManagedResourceReconciler
	in         chan event.TypedGenericEvent[Request]
	stop       func()

	// done is closed when the controller is stopped or fails to start.
	// An error is stored in startErr if this channel is closed.
	done chan struct{}

	// startErr holds any error encountered during Start().
	// If Start() returned nil, startErrAtomic will be ErrStopped.
	// startErr is only valid after done is closed.
	startErr error
}

func (rc *resourceController) StartErr() error {
	select {
	case <-rc.done:
		// controller has stopped, return any start error
		return rc.startErr
	default:
		// controller is still running
		return nil
	}
}

//+kubebuilder:rbac:groups=espejote.io,resources=managedresources,verbs=get;list;watch

func (r *ManagedResourceControllerManager) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceControllerManager.Reconcile")
	l.Info("Reconciling ManagedResource")

	var managedResource espejotev1alpha1.ManagedResource
	if err := r.Get(ctx, req.NamespacedName, &managedResource); err != nil {
		if apierrors.IsNotFound(err) {
			l.Info("ManagedResource is no longer available, stopping controller")
			r.stopAndRemoveControllerFor(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !managedResource.DeletionTimestamp.IsZero() {
		l.Info("ManagedResource is being deleted, stopping controller")
		r.stopAndRemoveControllerFor(req.NamespacedName)
		return ctrl.Result{}, nil
	}

	{
		ic, err := r.ensureInstanceControllerFor(ctx, managedResource)
		if err != nil {
			return ctrl.Result{}, multierr.Combine(
				fmt.Errorf("failed to ensure instance controller for managed resource %q: %w", req.NamespacedName, err),
				r.recordErr(ctx, fmt.Errorf("failed to ensure instance controller: %w", err), string(DependencyConfigurationError), managedResource, req),
			)
		}
		if err := ic.StartErr(); err != nil {
			return ctrl.Result{}, multierr.Combine(
				fmt.Errorf("failed to start instance controller for managed resource %q: %w", req.NamespacedName, err),
				r.recordErr(ctx, fmt.Errorf("failed to start instance controller: %w", err), string(ControllerStartError), managedResource, req),
			)
		}
		select {
		case ic.in <- event.TypedGenericEvent[Request]{Object: Request{}}:
		default:
		}
	}

	return ctrl.Result{}, nil
}

func (r *ManagedResourceControllerManager) recordErr(ctx context.Context, err error, defaultErrType string, managedResource espejotev1alpha1.ManagedResource, req reconcile.Request) error {
	errType := defaultErrType

	var espejoteErr EspejoteError
	if errors.As(err, &espejoteErr) {
		errType = string(espejoteErr.Type)
	}

	r.Recorder.Eventf(&managedResource, nil, "Warning", errType, "Reconcile", "%s: %s", errType, err.Error())
	var statusUpdateErr error
	if managedResource.Status.Status != errType {
		managedResource.Status.Status = errType
		if err := r.Status().Update(ctx, &managedResource); err != nil {
			statusUpdateErr = fmt.Errorf("failed to update status of managed resource %q: %w", req.NamespacedName, err)
		}
	}
	return statusUpdateErr
}

func (r *ManagedResourceControllerManager) ensureInstanceControllerFor(ctx context.Context, mr espejotev1alpha1.ManagedResource) (*resourceController, error) {
	l := log.FromContext(r.ControllerLifetimeCtx).WithName("ManagedResourceControllerManager.ensureInstanceControllerFor")

	mrKey := types.NamespacedName{
		Namespace: mr.GetNamespace(),
		Name:      mr.GetName(),
	}

	mrConfigHash, err := configHashForManagedResource(mr)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config hash for managed resource %q: %w", mrKey, err)
	}

	r.controllersMux.RLock()
	if c := r.controllers[mrKey]; c != nil && c.configHash == mrConfigHash {
		r.controllersMux.RUnlock()
		return c, nil
	}
	r.controllersMux.RUnlock()

	r.controllersMux.Lock()
	defer r.controllersMux.Unlock()
	if c := r.controllers[mrKey]; c != nil && c.configHash == mrConfigHash {
		return c, nil
	} else if c := r.controllers[mrKey]; c != nil {
		l.Info("Configuration changed, stopping existing controller", "oldHash", c.configHash, "newHash", mrConfigHash)
		t := time.Now()
		c.stop()
		<-c.done
		l.Info("Controller stopped", "duration", time.Since(t))
		delete(r.controllers, mrKey)
	}

	if mr.Status.Status != string(WaitingForCacheSync) {
		mr.Status.Status = string(WaitingForCacheSync)
		if err := r.Status().Update(ctx, &mr); err != nil {
			return nil, fmt.Errorf("failed to update status of managed resource %q: %w", mrKey, err)
		}
	}

	instanceCtrlCtx, instanceCtrlCancel := context.WithCancel(r.ControllerLifetimeCtx)
	reconciler := &ManagedResourceReconciler{
		For: mrKey,

		Client:   r.Client,
		Scheme:   r.Scheme,
		Recorder: r.Recorder,

		JsonnetLibraryNamespace: r.JsonnetLibraryNamespace,

		clientset:  r.clientset,
		restConfig: r.restConfig,
		mapper:     r.mapper,

		configHash:       mrConfigHash,
		configGeneration: mr.Generation,
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
			CacheSyncTimeout:   mr.Spec.CacheSyncTimeout.Duration,
		},
	)
	if err != nil {
		instanceCtrlCancel()
		return nil, fmt.Errorf("failed to create dynamic controller: %w", err)
	}
	instanceCtrl := &resourceController{
		configHash: mrConfigHash,

		in:         make(chan event.TypedGenericEvent[Request], 1),
		ctrl:       dynCtrl,
		stop:       instanceCtrlCancel,
		reconciler: reconciler,
		done:       make(chan struct{}),
	}
	c, err := r.cacheFor(ctx, instanceCtrlCtx, dynCtrl, mr)
	if err != nil {
		instanceCtrlCancel()
		return nil, fmt.Errorf("failed to create cache for managed resource %q: %w", mrKey, err)
	}
	reconciler.cache = c

	if err := dynCtrl.Watch(source.TypedChannel(instanceCtrl.in, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, event Request) []Request {
		return []Request{event}
	}))); err != nil {
		return nil, fmt.Errorf("failed to watch instance %q controller: %w", mrKey, err)
	}
	go func() {
		<-instanceCtrl.done
		r.notifyChan <- event.TypedGenericEvent[ctrl.Request]{Object: ctrl.Request{NamespacedName: mrKey}}
	}()

	go func() {
		err := dynCtrl.Start(instanceCtrlCtx)
		if err == nil {
			err = ErrStopped
		}
		instanceCtrl.startErr = err
		close(instanceCtrl.done)
	}()

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
	r.notifyChan = make(chan event.TypedGenericEvent[ctrl.Request])

	err := builder.ControllerManagedBy(mgr).
		For(&espejotev1alpha1.ManagedResource{}).
		WatchesRawSource(source.TypedChannel(r.notifyChan, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, event ctrl.Request) []ctrl.Request {
			return []ctrl.Request{event}
		}))).
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

// cacheFor returns the cache for the given ManagedResource.
// The caches life time is bound to lifetimeCtx.
// ctx is used for token requests and is not further used.
func (r *ManagedResourceControllerManager) cacheFor(ctx, lifetimeCtx context.Context, controller controller.TypedController[Request], mr espejotev1alpha1.ManagedResource) (*instanceCache, error) {
	rc, err := r.restConfigForManagedResource(ctx, mr)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for managed resource: %w", err)
	}

	if found, e := findFirstDuplicate(mr.Spec.Triggers, func(tr espejotev1alpha1.ManagedResourceTrigger) string { return tr.Name }); found {
		return nil, fmt.Errorf("duplicate trigger definition %q", e)
	}
	if found, e := findFirstDuplicate(mr.Spec.Context, func(tr espejotev1alpha1.ManagedResourceContext) string { return tr.Name }); found {
		return nil, fmt.Errorf("duplicate context definition %q", e)
	}

	ci := &instanceCache{
		triggerCaches: make(map[string]*definitionCache),
		contextCaches: make(map[string]*definitionCache),
	}
	for _, con := range mr.Spec.Context {
		if con.Resource.APIVersion == "" {
			return nil, fmt.Errorf("context %q has no resource", con.Name)
		}

		dc, err := r.newCacheForResourceAndRESTClient(lifetimeCtx, con.Resource, rc, mr.GetNamespace())
		if err != nil {
			return nil, fmt.Errorf("failed to create cache for resource %q: %w", con.Name, err)
		}

		ci.contextCaches[con.Name] = dc

		if err := controller.Watch(source.TypedKind(dc.cache, client.Object(dc.target), handler.TypedEnqueueRequestsFromMapFunc(func(context.Context, client.Object) []Request { return nil }))); err != nil {
			return nil, err
		}
	}

	for _, trigger := range mr.Spec.Triggers {
		if trigger.Interval.Duration != 0 {
			if err := r.setupIntervalTrigger(lifetimeCtx, controller, trigger.Interval.Duration, trigger.Name); err != nil {
				return nil, fmt.Errorf("failed to setup interval trigger %q: %w", trigger.Name, err)
			}
			continue
		}

		var defCache *definitionCache
		if trigger.WatchContextResource.Name != "" {
			dc, ok := ci.contextCaches[trigger.WatchContextResource.Name]
			if !ok {
				return nil, fmt.Errorf("context %q not found for trigger %q", trigger.WatchContextResource.Name, trigger.Name)
			}
			defCache = dc
		} else {
			if trigger.WatchResource.APIVersion == "" {
				return nil, fmt.Errorf("trigger %q has no watch resource or interval", trigger.Name)
			}

			dc, err := r.newCacheForResourceAndRESTClient(lifetimeCtx, trigger.WatchResource, rc, mr.GetNamespace())
			if err != nil {
				return nil, fmt.Errorf("failed to create cache for trigger %q: %w", trigger.Name, err)
			}
			defCache = dc
		}

		ci.triggerCaches[trigger.Name] = defCache

		if err := controller.Watch(source.TypedKind(defCache.cache, client.Object(defCache.target), handler.TypedEnqueueRequestsFromMapFunc(staticMapFunc(trigger.Name)))); err != nil {
			return nil, err
		}
	}

	return ci, nil
}

// restConfigForManagedResource returns a rest.Config for the given ManagedResource.
// The context is used to get a JWT token for the service account and is not further used.
// The rest.Config contains a Bearer token for the service account specified in the ManagedResource.
// The rest.Config copies the TLSClientConfig and Host from the controller's rest.Config.
func (r *ManagedResourceControllerManager) restConfigForManagedResource(ctx context.Context, mr espejotev1alpha1.ManagedResource) (*rest.Config, error) {
	name := mr.Spec.ServiceAccountRef.Name
	if name == "" {
		name = "default"
	}
	token, err := r.jwtTokenForSA(ctx, mr.GetNamespace(), name)
	if err != nil {
		return nil, fmt.Errorf("failed to get JWT token: %w", err)
	}

	// There's also a rest.CopyConfig function that could be used here
	config := rest.Config{
		WrapTransport:   r.restConfig.WrapTransport,
		Host:            r.restConfig.Host,
		BearerToken:     token,
		TLSClientConfig: *r.restConfig.TLSClientConfig.DeepCopy(),
	}
	return &config, nil
}

// jwtTokenForSA returns a JWT token for the given service account.
// The token is valid for 1 year.
func (r *ManagedResourceControllerManager) jwtTokenForSA(ctx context.Context, namespace, name string) (string, error) {
	treq, err := r.clientset.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, name, &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To(int64(60 * 60 * 24 * 365)), // 1 year
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", newEspejoteError(fmt.Errorf("token request for %q failed: %w", strings.Join([]string{"system:serviceaccount", namespace, name}, ":"), err), ServiceAccountError)
	}

	return treq.Status.Token, nil
}

// newCacheForResourceAndRESTClient creates a new cache for the given ClusterResource and REST client.
// The cache starts syncing in the background and is ready when CacheReady() is true on the returned cache.
func (r *ManagedResourceControllerManager) newCacheForResourceAndRESTClient(ctx context.Context, cr espejotev1alpha1.ClusterResource, rc *rest.Config, defaultNamespace string) (*definitionCache, error) {
	watchTarget := &unstructured.Unstructured{}
	watchTarget.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   cr.GetGroup(),
		Version: cr.GetVersion(),
		Kind:    cr.GetKind(),
	})

	isNamespaced, err := apiutil.IsObjectNamespaced(watchTarget, r.Scheme, r.mapper)
	if err != nil {
		return nil, fmt.Errorf("failed to determine if watch target %q is namespaced: %w", cr, err)
	}

	var sel []fields.Selector
	if name := cr.GetName(); name != "" {
		sel = append(sel, fields.OneTermEqualSelector("metadata.name", name))
	}
	if ns := ptr.Deref(cr.GetNamespace(), defaultNamespace); isNamespaced && ns != "" {
		sel = append(sel, fields.OneTermEqualSelector("metadata.namespace", ns))
	}

	lblSel := labels.Everything()
	if cr.GetLabelSelector() != nil {
		s, err := metav1.LabelSelectorAsSelector(cr.GetLabelSelector())
		if err != nil {
			return nil, fmt.Errorf("failed to parse label selector for trigger %q: %w", cr, err)
		}
		lblSel = s
	}

	filterInf := toolscache.NewSharedIndexInformer
	if len(cr.GetMatchNames()) > 0 || len(cr.GetIgnoreNames()) > 0 {
		filterInf = wrapNewInformerWithFilter(func(o client.Object) bool {
			return (len(cr.GetMatchNames()) == 0 || slices.Contains(cr.GetMatchNames(), o.GetName())) &&
				!slices.Contains(cr.GetIgnoreNames(), o.GetName())
		})
	}

	var transformFunc toolscache.TransformFunc
	if cr.GetStripManagedFields() {
		transformFunc = cache.TransformStripManagedFields()
	}

	c, err := cache.New(rc, cache.Options{
		Scheme: r.Scheme,
		Mapper: r.mapper,
		// We want to explicitly fail if the informer is missing, otherwise we might create some unconfigured caches without any warning on programming errors.
		ReaderFailOnMissingInformer: true,
		DefaultFieldSelector:        fields.AndSelectors(sel...),
		DefaultLabelSelector:        lblSel,
		// We don't want to deep copy the objects, as we don't modify them
		// This is mostly to make metric collection more efficient
		DefaultUnsafeDisableDeepCopy: ptr.To(true),

		DefaultTransform: transformFunc,

		NewInformer: filterInf,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cache for trigger %q: %w", cr, err)
	}

	dc := &definitionCache{
		target: watchTarget,
		cache:  c,
	}

	go c.Start(ctx)

	return dc, nil
}

// setupIntervalTrigger sets up a trigger that fires every interval.
// The trigger is enqueued with the ManagedResource's key and the trigger index.
// The trigger is shut down when the context is canceled.
func (r *ManagedResourceControllerManager) setupIntervalTrigger(lifetimeCtx context.Context, controller controller.TypedController[Request], interval time.Duration, triggerName string) error {
	return controller.Watch(source.TypedFunc[Request](func(ctx context.Context, queue workqueue.TypedRateLimitingInterface[Request]) error {
		tick := time.NewTicker(interval)
		go func() {
			defer tick.Stop()

			// merge cancellation signals from interval lifetime context and context given by the controller
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			context.AfterFunc(lifetimeCtx, cancel)

			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					queue.Add(Request{
						TriggerInfo: TriggerInfo{
							TriggerName: triggerName,
						},
					})
				}
			}
		}()
		return nil
	}))
}

func configHashForManagedResource(mr espejotev1alpha1.ManagedResource) (string, error) {
	hsh := md5.New()
	henc := jsontext.NewEncoder(hsh)
	if err := json.MarshalEncode(henc, mr.Spec.Triggers, json.Deterministic(true)); err != nil {
		return "", fmt.Errorf("failed to encode triggers: %w", err)
	}
	if err := json.MarshalEncode(henc, mr.Spec.Context, json.Deterministic(true)); err != nil {
		return "", fmt.Errorf("failed to encode contexts: %w", err)
	}
	if _, err := io.WriteString(hsh, mr.Spec.ServiceAccountRef.Name); err != nil {
		return "", fmt.Errorf("failed to hash service account name: %w", err)
	}
	configHash := fmt.Sprintf("%x", hsh.Sum(nil))
	return configHash, nil
}

func staticMapFunc(triggerName string) func(context.Context, client.Object) []Request {
	return func(_ context.Context, o client.Object) []Request {
		gvk := o.GetObjectKind().GroupVersionKind()
		return []Request{
			{
				TriggerInfo: TriggerInfo{
					TriggerName: triggerName,

					WatchResource: WatchResource{
						APIVersion: gvk.Version,
						Group:      gvk.Group,
						Kind:       gvk.Kind,
						Name:       o.GetName(),
						Namespace:  o.GetNamespace(),
					},
				},
			},
		}
	}
}

// wrapNewInformerWithFilter wraps the NewSharedIndexInformer function to filter the informer's results.
// The filter function is called for each object that can be cast to runtime.Object returned by the informer.
// If the function returns false, the object is filtered out.
// If the object can't be cast to client.Object, it is not filtered out.
// Warning: I'm not sure if this is a good idea or if it can lead to inconsistencies.
// You must NOT filter the object by fields that can be modified, as watch can't properly update the Type field (Add/Modified/Deleted) to reflect items beginning to pass the filter when they previously didn't.
func wrapNewInformerWithFilter(f func(o client.Object) (keep bool)) func(toolscache.ListerWatcher, runtime.Object, time.Duration, toolscache.Indexers) toolscache.SharedIndexInformer {
	return func(lw toolscache.ListerWatcher, o runtime.Object, d time.Duration, i toolscache.Indexers) toolscache.SharedIndexInformer {
		lwc := toolscache.ToListerWatcherWithContext(lw)
		flw := &toolscache.ListWatch{
			WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
				i, err := lwc.WatchWithContext(ctx, options)
				if err != nil {
					return nil, err
				}
				return watch.Filter(i, func(in watch.Event) (out watch.Event, keep bool) {
					obj, ok := in.Object.(client.Object)
					if !ok {
						return in, true
					}
					return in, f(obj)
				}), nil
			},
			ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
				list, err := lwc.ListWithContext(ctx, options)
				if err != nil {
					return nil, err
				}

				items, err := meta.ExtractListWithAlloc(list)
				if err != nil {
					return list, fmt.Errorf("unable to understand list result %#v (%v)", list, err)
				}

				filtered := slices.DeleteFunc(items, func(ro runtime.Object) bool {
					obj, ok := ro.(client.Object)
					if !ok {
						return false
					}
					return !f(obj)
				})

				if err := meta.SetList(list, filtered); err != nil {
					return list, fmt.Errorf("unable to set filtered list result %#v (%v)", list, err)
				}

				return list, nil
			},
		}

		return toolscache.NewSharedIndexInformer(flw, o, d, i)
	}
}

// findFirstDuplicate finds the first duplicate element in the given slice.
// Returns true if a duplicate was found and the duplicate element.
func findFirstDuplicate[T any, E comparable](s []T, f func(T) E) (found bool, duplicate E) {
	cns := sets.New[E]()
	for _, el := range s {
		v := f(el)
		if cns.Has(v) {
			return true, v
		}
		cns.Insert(v)
	}
	return false, duplicate
}
