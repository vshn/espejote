package controllers

import (
	"context"
	"crypto/md5"
	encjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DmitriyVTitov/size"
	"github.com/google/go-jsonnet"
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
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/plugins"
)

const jsonNull = "null"

type Request struct {
	TriggerInfo TriggerInfo
}

type TriggerInfo struct {
	TriggerName string

	WatchResource WatchResource
}

type WatchResource struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Group      string `json:"group,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
}

type ManagedResourceReconciler struct {
	// For identifies the managed resource being reconciled.
	// This reconciler does not work like a classic reconciler.
	// One instance of this reconciler exists for each managed resource and it only ever reconciles that resource.
	// ManagedResourceControllerManager dynamically creates and manages these reconciler instances.
	For types.NamespacedName

	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	ControllerLifetimeCtx   context.Context
	JsonnetLibraryNamespace string

	PluginManager *plugins.Manager

	controller controller.TypedController[Request]
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	mapper     meta.RESTMapper

	cacheMux sync.RWMutex
	cache    *instanceCache

	// newCacheFunc is only used for testing
	newCacheFunction cache.NewCacheFunc
}

type instanceCache struct {
	triggerCaches map[string]*definitionCache
	contextCaches map[string]*definitionCache

	watchConfigHash string

	stop func()
}

func (ci *instanceCache) Stop() {
	ci.stop()
}

// clientForTrigger returns a client.Reader for the given trigger.
func (ci *instanceCache) clientForTrigger(triggerName string) (client.Reader, error) {
	dc, ok := ci.triggerCaches[triggerName]
	if !ok {
		return nil, fmt.Errorf("cache for trigger %q not found", triggerName)
	}
	return dc.cache, nil
}

// clientForContext returns a client.Reader for the given context.
func (ci *instanceCache) clientForContext(contextName string) (client.Reader, error) {
	dc, ok := ci.contextCaches[contextName]
	if !ok {
		return nil, fmt.Errorf("cache for context %q not found", contextName)
	}
	return dc.cache, nil
}

func (ci *instanceCache) AllCachesReady() (bool, error) {
	ret := true
	errs := make([]error, 0, len(ci.triggerCaches))
	for _, dc := range ci.triggerCaches {
		ready, err := dc.CacheReady()
		ret = ret && ready
		errs = append(errs, err)
	}
	for _, dc := range ci.contextCaches {
		ready, err := dc.CacheReady()
		ret = ret && ready
		errs = append(errs, err)
	}
	return ret, multierr.Combine(errs...)
}

type definitionCache struct {
	target *unstructured.Unstructured

	cache cache.Cache

	cacheReadyMux sync.Mutex
	cacheReady    error
}

func (ci *definitionCache) CacheReady() (bool, error) {
	ci.cacheReadyMux.Lock()
	defer ci.cacheReadyMux.Unlock()
	return ci.cacheReady == nil, ignoreErrCacheNotReady(ci.cacheReady)
}

// Size returns the number of objects and the size of the cache in bytes.
// The size is an approximation and may not be accurate.
// The size is calculated by listing all objects in the cache and recursively adding the reflect.Size of each object.
// The assumption is that the objects are the biggest part of the cache.
// TODO: size.Of does a bunch of reflect and some allocations, we might want to simplify/optimize this. I never benchmarked it.
func (ci *definitionCache) Size(ctx context.Context) (int, int, error) {
	if _, err := ci.CacheReady(); err != nil {
		return 0, 0, err
	}

	list, err := ci.target.DeepCopy().ToList()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build list from target object: %w", err)
	}
	if err := ci.cache.List(ctx, list); err != nil {
		return 0, 0, fmt.Errorf("failed to list objects: %w", err)
	}

	return len(list.Items), size.Of(list), nil
}

func ignoreErrCacheNotReady(err error) error {
	if errors.Is(err, ErrCacheNotReady) {
		return nil
	}
	return err
}

//+kubebuilder:rbac:groups=espejote.io,resources=managedresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=espejote.io,resources=managedresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=espejote.io,resources=managedresources/finalizers,verbs=update

//+kubebuilder:rbac:groups=espejote.io,resources=jsonnetlibraries,verbs=get;list;watch

//+kubebuilder:rbac:groups="",resources=serviceaccounts/token,verbs=create
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type EspejoteErrorType string

const (
	ServiceAccountError          EspejoteErrorType = "ServiceAccountError"
	WaitingForCacheSync          EspejoteErrorType = "WaitingForCacheSync"
	DependencyConfigurationError EspejoteErrorType = "DependencyConfigurationError"
	TemplateError                EspejoteErrorType = "TemplateError"
	ApplyError                   EspejoteErrorType = "ApplyError"
	TemplateReturnError          EspejoteErrorType = "TemplateReturnError"
	ControllerInstantiationError EspejoteErrorType = "ControllerInstantiationError"
)

type EspejoteError struct {
	error
	Type EspejoteErrorType
}

// ErrorIsTransient returns true if the error is transient and the reconciliation should be retried without an error log and backoff.
func ErrorIsTransient(err error) bool {
	var espejoteErr EspejoteError
	return errors.As(err, &espejoteErr) && espejoteErr.Transient()
}

// Transient returns true if the error is transient and the reconciliation should be retried without an error log and backoff.
func (e EspejoteError) Transient() bool {
	return e.Type == WaitingForCacheSync
}

func (e EspejoteError) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.error)
}

func newEspejoteError(err error, t EspejoteErrorType) EspejoteError {
	return EspejoteError{err, t}
}

func (r *ManagedResourceReconciler) Reconcile(ctx context.Context, req Request) (ctrl.Result, error) {
	// Since we are not using the default builder we need to add the request to the context ourselves
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("request", req))

	reconciles.WithLabelValues(r.For.Name, r.For.Namespace, req.TriggerInfo.TriggerName).Inc()

	res, recErr := r.reconcile(ctx, req)

	result := res
	resultErr := recErr
	if ErrorIsTransient(recErr) {
		result = ctrl.Result{Requeue: true}
		resultErr = nil
	}

	return result, multierr.Combine(resultErr, r.recordReconcileErr(ctx, req, recErr))
}

func (r *ManagedResourceReconciler) reconcile(ctx context.Context, req Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceReconciler.reconcile")
	l.Info("Reconciling ManagedResource")

	var managedResource espejotev1alpha1.ManagedResource
	if err := r.Get(ctx, r.For, &managedResource); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	if !managedResource.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	ci, err := r.cacheFor(ctx, managedResource)
	if err != nil {
		return ctrl.Result{}, newEspejoteError(err, DependencyConfigurationError)
	}
	ready, err := ci.AllCachesReady()
	if err != nil {
		return ctrl.Result{}, newEspejoteError(err, DependencyConfigurationError)
	}
	if !ready {
		return ctrl.Result{}, newEspejoteError(ErrCacheNotReady, WaitingForCacheSync)
	}

	rendered, err := (&Renderer{
		Importer:            FromClientImporter(r.Client, managedResource.GetNamespace(), r.JsonnetLibraryNamespace),
		PluginManager:       r.PluginManager,
		TriggerClientGetter: ci.clientForTrigger,
		ContextClientGetter: ci.clientForContext,
	}).Render(ctx, managedResource, req.TriggerInfo)
	if err != nil {
		return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to render template: %w", err), TemplateError)
	}

	objects, err := r.unpackRenderedObjects(ctx, rendered)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to unpack rendered objects: %w", err)
	}

	// ensure namespaced objects have a namespace set
	for _, obj := range objects {
		namespaced, err := apiutil.IsObjectNamespaced(obj, r.Scheme, r.mapper)
		if err != nil {
			return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to determine if object is namespaced: %w", err), ApplyError)
		}
		if namespaced && obj.GetNamespace() == "" {
			obj.SetNamespace(managedResource.GetNamespace())
		}
	}

	// apply objects returned by the template
	c, err := r.uncachedClientForManagedResource(ctx, managedResource)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get client for managed resource: %w", err)
	}

	applyErrs := make([]error, 0, len(objects))
	for _, obj := range objects {
		// check if object is marked for deletion
		shouldDelete, opts, err := deleteOptionsFromRenderedObject(obj)
		if err != nil {
			applyErrs = append(applyErrs, fmt.Errorf("failed to get deletion flag: %w", err))
			continue
		}
		if shouldDelete {
			if err := c.Delete(ctx, stripUnstructuredForDelete(obj), opts...); client.IgnoreNotFound(err) != nil {
				applyErrs = append(applyErrs, fmt.Errorf("failed to delete object %q %q: %w", obj.GetObjectKind(), obj.GetName(), err))
			}
			continue
		}

		patchOptions, err := patchOptionsFromObject(managedResource, obj)
		if err != nil {
			applyErrs = append(applyErrs, fmt.Errorf("failed to get patch options: %w", err))
			continue
		}
		if err := c.Patch(ctx, obj, client.Apply, patchOptions...); err != nil {
			applyErrs = append(applyErrs, fmt.Errorf("failed to apply object %q %q: %w", obj.GetObjectKind(), obj.GetName(), err))
		}
	}
	if err := multierr.Combine(applyErrs...); err != nil {
		return ctrl.Result{}, newEspejoteError(err, ApplyError)
	}

	return ctrl.Result{}, nil
}

// recordReconcileErr records the given error as an event on the ManagedResource and updates the status accordingly.
// If the error is nil, the status is updated to "Ready".
// If the error is a transient error, it is not recorded as an event or metric.
func (r *ManagedResourceReconciler) recordReconcileErr(ctx context.Context, req Request, recErr error) error {
	var managedResource espejotev1alpha1.ManagedResource
	if err := r.Get(ctx, r.For, &managedResource); err != nil {
		return client.IgnoreNotFound(err)
	}

	// record success status if no error
	if recErr == nil {
		if managedResource.Status.Status == "Ready" {
			return nil
		}
		managedResource.Status.Status = "Ready"
		return r.Status().Update(ctx, &managedResource)
	}

	errType := "ReconcileError"
	transient := false
	var espejoteErr EspejoteError
	if errors.As(recErr, &espejoteErr) {
		errType = string(espejoteErr.Type)
		transient = espejoteErr.Transient()
	}

	if !transient {
		r.Recorder.Eventf(&managedResource, "Warning", errType, "Reconcile error: %s", recErr.Error())
		reconcileErrors.WithLabelValues(r.For.Name, r.For.Namespace, req.TriggerInfo.TriggerName, errType).Inc()
	}

	if managedResource.Status.Status == errType {
		return nil
	}

	managedResource.Status.Status = errType
	return r.Status().Update(ctx, &managedResource)
}

var ErrCacheNotReady = errors.New("cache not ready")
var ErrFailedSyncCache = errors.New("failed to sync cache")

// cacheFor returns the cache for the given ManagedResource.
// A new cache is created if it does not exist yet or if the configuration changed.
// The cache is stopped if the configuration changed.
func (r *ManagedResourceReconciler) cacheFor(ctx context.Context, mr espejotev1alpha1.ManagedResource) (*instanceCache, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceReconciler.cacheFor")

	k := client.ObjectKeyFromObject(&mr)

	hsh := md5.New()
	henc := json.NewEncoder(hsh)
	if err := henc.Encode(mr.Spec.Triggers); err != nil {
		return nil, fmt.Errorf("failed to encode triggers: %w", err)
	}
	if err := henc.Encode(mr.Spec.Context); err != nil {
		return nil, fmt.Errorf("failed to encode contexts: %w", err)
	}
	if _, err := io.WriteString(hsh, mr.Spec.ServiceAccountRef.Name); err != nil {
		return nil, fmt.Errorf("failed to hash service account name: %w", err)
	}
	configHash := fmt.Sprintf("%x", hsh.Sum(nil))

	r.cacheMux.RLock()
	if cache := r.cache; cache != nil && cache.watchConfigHash == configHash {
		r.cacheMux.RUnlock()
		return cache, nil
	}
	r.cacheMux.RUnlock()

	r.cacheMux.Lock()
	defer r.cacheMux.Unlock()

	if cache := r.cache; cache != nil && cache.watchConfigHash == configHash {
		return cache, nil
	} else if cache != nil {
		l.Info("cache config changed, stopping old cache", "old", cache.watchConfigHash, "new", configHash)
		cache.Stop()
	}

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

	cctx, cancel := context.WithCancel(r.ControllerLifetimeCtx)
	ci := &instanceCache{
		triggerCaches:   make(map[string]*definitionCache),
		contextCaches:   make(map[string]*definitionCache),
		stop:            cancel,
		watchConfigHash: configHash,
	}
	for _, con := range mr.Spec.Context {
		if con.Plugin.Name != "" {
			continue
		}
		if con.Resource.APIVersion == "" {
			return nil, fmt.Errorf("context %q has no resource", con.Name)
		}

		dc, err := r.newCacheForResourceAndRESTClient(cctx, con.Resource, rc, mr.GetNamespace())
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create cache for resource %q: %w", con.Name, err)
		}

		ci.contextCaches[con.Name] = dc

		// We only allow querying objects with existing informers
		if _, err := dc.cache.GetInformer(cctx, dc.target); err != nil {
			cancel()
			return nil, err
		}
	}

	for _, trigger := range mr.Spec.Triggers {
		if trigger.Interval.Duration != 0 {
			if err := r.setupIntervalTrigger(cctx, trigger.Interval.Duration, k, trigger.Name); err != nil {
				cancel()
				return nil, fmt.Errorf("failed to setup interval trigger %q: %w", trigger.Name, err)
			}
			continue
		}

		var defCache *definitionCache
		if trigger.WatchContextResource.Name != "" {
			dc, ok := ci.contextCaches[trigger.WatchContextResource.Name]
			if !ok {
				cancel()
				return nil, fmt.Errorf("context %q not found for trigger %q", trigger.WatchContextResource.Name, trigger.Name)
			}
			defCache = dc
		} else {
			if trigger.WatchResource.APIVersion == "" {
				return nil, fmt.Errorf("trigger %q has no watch resource or interval", trigger.Name)
			}

			dc, err := r.newCacheForResourceAndRESTClient(cctx, trigger.WatchResource, rc, mr.GetNamespace())
			if err != nil {
				cancel()
				return nil, fmt.Errorf("failed to create cache for trigger %q: %w", trigger.Name, err)
			}
			defCache = dc
		}

		ci.triggerCaches[trigger.Name] = defCache

		if err := r.controller.Watch(source.TypedKind(defCache.cache, client.Object(defCache.target), handler.TypedEnqueueRequestsFromMapFunc(staticMapFunc(client.ObjectKeyFromObject(&mr), trigger.Name)))); err != nil {
			cancel()
			return nil, err
		}
	}

	r.cache = ci
	return ci, nil
}

func (r *ManagedResourceReconciler) getCache() *instanceCache {
	r.cacheMux.RLock()
	defer r.cacheMux.RUnlock()
	return r.cache
}

// Render renders the given ManagedResource.
type Renderer struct {
	Importer jsonnet.Importer

	PluginManager *plugins.Manager

	TriggerClientGetter func(triggerName string) (client.Reader, error)
	ContextClientGetter func(contextName string) (client.Reader, error)
}

// Render renders the given ManagedResource.
func (r *Renderer) Render(ctx context.Context, managedResource espejotev1alpha1.ManagedResource, ti TriggerInfo) (string, error) {
	jvm := jsonnet.MakeVM()
	jvm.Importer(r.Importer)

	triggerJSON, err := r.renderTriggers(ctx, r.TriggerClientGetter, ti)
	if err != nil {
		return "", fmt.Errorf("failed to render triggers: %w", err)
	}
	// Mark as internal so we can change the implementation later, to make it more efficient for example
	jvm.ExtCode("__internal_use_espejote_lib_trigger", triggerJSON)

	contextJSON, err := r.renderContexts(ctx, r.ContextClientGetter, managedResource)
	if err != nil {
		return "", fmt.Errorf("failed to render contexts: %w", err)
	}
	jvm.ExtCode("__internal_use_espejote_lib_context", contextJSON)

	rendered, err := jvm.EvaluateAnonymousSnippet("template", managedResource.Spec.Template)
	if err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return strings.Trim(rendered, " \t\r\n"), nil
}

// renderTriggers renders the trigger information for the given Request.
func (r *Renderer) renderTriggers(ctx context.Context, getReader func(string) (client.Reader, error), ti TriggerInfo) (string, error) {
	triggerInfo := struct {
		Name string             `json:"name,omitempty"`
		Data encjson.RawMessage `json:"data,omitempty"`
	}{
		Name: ti.TriggerName,
	}

	if ti.WatchResource != (WatchResource{}) {
		var triggerObj unstructured.Unstructured
		triggerObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   ti.WatchResource.Group,
			Version: ti.WatchResource.APIVersion,
			Kind:    ti.WatchResource.Kind,
		})
		reader, err := getReader(ti.TriggerName)
		if err != nil {
			return "", fmt.Errorf("failed to get reader for trigger: %w", err)
		}
		err = reader.Get(ctx, types.NamespacedName{Namespace: ti.WatchResource.Namespace, Name: ti.WatchResource.Name}, &triggerObj)
		if err == nil {
			triggerJSONBytes, err := json.Marshal(struct {
				Resource map[string]any `json:"resource"`
			}{
				Resource: triggerObj.UnstructuredContent(),
			})
			if err != nil {
				return "", fmt.Errorf("failed to marshal trigger info: %w", err)
			}
			triggerInfo.Data = triggerJSONBytes
		} else if apierrors.IsNotFound(err) {
			log.FromContext(ctx).WithValues("trigger", ti.WatchResource).Info("trigger object not found, was deleted")
		} else {
			return "", fmt.Errorf("failed to get trigger object: %w", err)
		}
	}

	triggerJSON, err := json.Marshal(triggerInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal trigger info: %w", err)
	}
	return string(triggerJSON), nil
}

// renderContexts renders the context information for the given ManagedResource.
func (r *Renderer) renderContexts(ctx context.Context, getReader func(string) (client.Reader, error), managedResource espejotev1alpha1.ManagedResource) (string, error) {
	contexts := map[string]any{}
	for _, con := range managedResource.Spec.Context {
		if con.Plugin.Name != "" {
			b, err := json.Marshal(con.Plugin.Data)
			if err != nil {
				return "", fmt.Errorf("failed to marshal plugin data for context %q: %w", con.Name, err)
			}
			if r.PluginManager == nil {
				return "", fmt.Errorf("plugins are not enabled, cannot run plugin %q for context %q", con.Plugin.Name, con.Name)
			}
			stdout, stderr, err := r.PluginManager.RunPlugin(ctx, con.Plugin.Name, []string{string(b)})
			if err != nil {
				return "", fmt.Errorf("failed to run plugin %q for context %q: %w\nstdout: %s\nstderr\n: %s", con.Plugin.Name, con.Name, err, stdout, stderr)
			}
			contexts[con.Name] = stdout
			continue
		}

		if con.Resource.APIVersion == "" {
			continue
		}
		reader, err := getReader(con.Name)
		if err != nil {
			return "", fmt.Errorf("failed to get reader for context: %w", err)
		}
		var contextObj unstructured.Unstructured
		contextObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   con.Resource.GetGroup(),
			Version: con.Resource.GetVersion(),
			Kind:    con.Resource.GetKind(),
		})
		contextObjs, err := contextObj.ToList()
		if err != nil {
			return "", fmt.Errorf("failed to build context list: %w", err)
		}
		if err := reader.List(ctx, contextObjs); err != nil {
			return "", fmt.Errorf("failed to list context objects: %w", err)
		}

		unwrapped := make([]any, len(contextObjs.Items))
		for i, obj := range contextObjs.Items {
			unwrapped[i] = obj.UnstructuredContent()
		}
		contexts[con.Name] = unwrapped
	}
	contextJSON, err := json.Marshal(contexts)
	if err != nil {
		return "", fmt.Errorf("failed to marshal contexts: %w", err)
	}
	return string(contextJSON), nil
}

// uncachedClientForManagedResource returns a client.Client running in the context of the managed resource's service account.
// It does not add any caches.
// The context is used to get a JWT token for the service account and is not further used.
func (r *ManagedResourceReconciler) uncachedClientForManagedResource(ctx context.Context, mr espejotev1alpha1.ManagedResource) (client.Client, error) {
	rc, err := r.restConfigForManagedResource(ctx, mr)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for managed resource: %w", err)
	}

	return client.New(rc, client.Options{
		Scheme: r.Scheme,
		Mapper: r.mapper,
	})
}

// unpackRenderedObjects unpacks the rendered template into a list of client.Objects.
// The rendered template can be a single object, a list of objects or null.
func (r *ManagedResourceReconciler) unpackRenderedObjects(ctx context.Context, rendered string) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	if rendered != "" && strings.HasPrefix(rendered, "{") {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON([]byte(rendered)); err != nil {
			return nil, newEspejoteError(fmt.Errorf("failed to unmarshal rendered template: %w", err), TemplateReturnError)
		}
		objects = append(objects, obj)
	} else if rendered != "" && strings.HasPrefix(rendered, "[") {
		// RawMessage is used to delay unmarshaling
		var list []encjson.RawMessage
		if err := json.Unmarshal([]byte(rendered), &list); err != nil {
			return nil, newEspejoteError(fmt.Errorf("failed to unmarshal rendered template: %w", err), TemplateReturnError)
		}
		for _, raw := range list {
			obj := &unstructured.Unstructured{}
			if err := obj.UnmarshalJSON(raw); err != nil {
				return nil, newEspejoteError(fmt.Errorf("failed to unmarshal rendered template: %w", err), TemplateReturnError)
			}
			objects = append(objects, obj)
		}
	} else if rendered == jsonNull {
		log.FromContext(ctx).Info("template returned null, no objects to apply")
	} else {
		return nil, newEspejoteError(fmt.Errorf("unexpected output from rendered template: %q", rendered), TemplateReturnError)
	}

	return objects, nil
}

// jwtTokenForSA returns a JWT token for the given service account.
// The token is valid for 1 year.
func (r *ManagedResourceReconciler) jwtTokenForSA(ctx context.Context, namespace, name string) (string, error) {
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

// restConfigForManagedResource returns a rest.Config for the given ManagedResource.
// The context is used to get a JWT token for the service account and is not further used.
// The rest.Config contains a Bearer token for the service account specified in the ManagedResource.
// The rest.Config copies the TLSClientConfig and Host from the controller's rest.Config.
func (r *ManagedResourceReconciler) restConfigForManagedResource(ctx context.Context, mr espejotev1alpha1.ManagedResource) (*rest.Config, error) {
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
		Host:            r.restConfig.Host,
		BearerToken:     token,
		TLSClientConfig: *r.restConfig.TLSClientConfig.DeepCopy(),
	}
	return &config, nil
}

// newCacheForResourceAndRESTClient creates a new cache for the given ClusterResource and REST client.
// The cache starts syncing in the background and is ready when CacheReady() is true on the returned cache.
func (r *ManagedResourceReconciler) newCacheForResourceAndRESTClient(ctx context.Context, cr espejotev1alpha1.ClusterResource, rc *rest.Config, defaultNamespace string) (*definitionCache, error) {
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

	newCacheFunc := r.newCacheFunction
	if newCacheFunc == nil {
		newCacheFunc = cache.New
	}

	c, err := newCacheFunc(rc, cache.Options{
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
		target:     watchTarget,
		cache:      c,
		cacheReady: ErrCacheNotReady,
	}

	go c.Start(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		success := c.WaitForCacheSync(ctx)
		dc.cacheReadyMux.Lock()
		defer dc.cacheReadyMux.Unlock()

		if success {
			dc.cacheReady = nil
		} else {
			dc.cacheReady = fmt.Errorf("failed to sync cache for %q: %w", watchTarget.GroupVersionKind(), ErrFailedSyncCache)
		}
	}()

	return dc, nil
}

// setupIntervalTrigger sets up a trigger that fires every interval.
// The trigger is enqueued with the ManagedResource's key and the trigger index.
// The trigger is shut down when the context is canceled.
func (r *ManagedResourceReconciler) setupIntervalTrigger(lifetimeCtx context.Context, interval time.Duration, mrKey types.NamespacedName, triggerName string) error {
	return r.controller.Watch(source.TypedFunc[Request](func(ctx context.Context, queue workqueue.TypedRateLimitingInterface[Request]) error {
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

func staticMapFunc(r types.NamespacedName, triggerName string) func(context.Context, client.Object) []Request {
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
		flw := &toolscache.ListWatch{
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				i, err := lw.Watch(options)
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
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				list, err := lw.List(options)
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

// patchOptionsFromObject returns the patch options for the given ManagedResource and object.
// The options are merged from the ManagedResource's ApplyOptions and the object's annotations.
// Object annotations take precedence over ManagedResource's ApplyOptions.
// The options are:
// - FieldValidation: the field validation mode (default: "Strict")
// - FieldManager: the field manager/owner (default: "managed-resource:<name>")
// - ForceOwnership: if true, the ownership is forced (default: false)
// Warning: this function modifies the object by removing the options from the annotations.
func patchOptionsFromObject(mr espejotev1alpha1.ManagedResource, obj *unstructured.Unstructured) ([]client.PatchOption, error) {
	const optionsKey = "__internal_use_espejote_lib_apply_options"

	fieldValidation := mr.Spec.ApplyOptions.FieldValidation
	objFieldValidation, ok, err := unstructured.NestedString(obj.UnstructuredContent(), optionsKey, "fieldValidation")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option field validation: %w", err)
	}
	if ok {
		fieldValidation = objFieldValidation
	}
	if fieldValidation == "" {
		fieldValidation = "Strict"
	}

	fieldManager := mr.Spec.ApplyOptions.FieldManager
	objFieldManager, ok, err := unstructured.NestedString(obj.UnstructuredContent(), optionsKey, "fieldManager")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option fieldManager: %w", err)
	}
	if ok {
		fieldManager = objFieldManager
	}
	if fieldManager == "" {
		fieldManager = fmt.Sprintf("managed-resource:%s", mr.GetName())
	}
	objFieldManagerSuffix, _, err := unstructured.NestedString(obj.UnstructuredContent(), optionsKey, "fieldManagerSuffix")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option fieldManagerSuffix: %w", err)
	}
	fieldManager += objFieldManagerSuffix

	po := []client.PatchOption{client.FieldValidation(fieldValidation), client.FieldOwner(fieldManager)}

	objForce, ok, err := unstructured.NestedBool(obj.UnstructuredContent(), optionsKey, "force")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option force: %w", err)
	}
	if ok {
		if objForce {
			po = append(po, client.ForceOwnership)
		}
	} else if mr.Spec.ApplyOptions.Force {
		po = append(po, client.ForceOwnership)
	}

	unstructured.RemoveNestedField(obj.UnstructuredContent(), optionsKey)

	return po, nil
}

// stripUnstructuredForDelete returns a copy of the given unstructured object with only the GroupVersionKind, Namespace and Name set.
func stripUnstructuredForDelete(u *unstructured.Unstructured) *unstructured.Unstructured {
	cp := &unstructured.Unstructured{}
	cp.SetGroupVersionKind(u.GroupVersionKind())
	cp.SetNamespace(u.GetNamespace())
	cp.SetName(u.GetName())
	return cp
}

// deleteOptionsFromRenderedObject extracts the deletion options from the given unstructured object.
// The deletion options are stored in the object under the "__internal_use_espejote_lib_deletion" key.
// The deletion options are:
// - delete: bool, required, if true the object should be deleted
// - gracePeriodSeconds: int, optional, the grace period for the deletion
// - propagationPolicy: string, optional, the deletion propagation policy
// - preconditionUID: string, optional, the UID of the object that must match for deletion
// - preconditionResourceVersion: string, optional, the resource version of the object that must match for deletion
// The first return value is true if the object should be deleted.
func deleteOptionsFromRenderedObject(obj *unstructured.Unstructured) (shouldDelete bool, opts []client.DeleteOption, err error) {
	const deletionKey = "__internal_use_espejote_lib_deletion"

	shouldDelete, _, err = unstructured.NestedBool(obj.UnstructuredContent(), deletionKey, "delete")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion flag: %w", err)
	}
	if !shouldDelete {
		return false, nil, nil
	}

	gracePeriodSeconds, ok, err := unstructured.NestedInt64(obj.UnstructuredContent(), deletionKey, "gracePeriodSeconds")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion grace period: %w", err)
	}
	if ok {
		opts = append(opts, client.GracePeriodSeconds(gracePeriodSeconds))
	}

	propagationPolicy, ok, err := unstructured.NestedString(obj.UnstructuredContent(), deletionKey, "propagationPolicy")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion propagation policy: %w", err)
	}
	if ok {
		opts = append(opts, client.PropagationPolicy(metav1.DeletionPropagation(propagationPolicy)))
	}

	preconditions := metav1.Preconditions{}
	hasPreconditions := false
	preconditionUID, ok, err := unstructured.NestedString(obj.UnstructuredContent(), deletionKey, "preconditionUID")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion precondition UID: %w", err)
	}
	if ok {
		hasPreconditions = true
		preconditions.UID = ptr.To(types.UID(preconditionUID))
	}
	preconditionResourceVersion, ok, err := unstructured.NestedString(obj.UnstructuredContent(), deletionKey, "preconditionResourceVersion")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion precondition resource version: %w", err)
	}
	if ok {
		hasPreconditions = true
		preconditions.ResourceVersion = &preconditionResourceVersion
	}
	if hasPreconditions {
		opts = append(opts, client.Preconditions(preconditions))
	}

	return
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
