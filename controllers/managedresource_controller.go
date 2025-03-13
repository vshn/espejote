package controllers

import (
	"context"
	"crypto/md5"
	encjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/source"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

const jsonNull = "null"

type Request = struct {
	NamespacedName types.NamespacedName
	// Include more trigger info
	TriggerInfo TriggerInfo
}

type TriggerInfo struct {
	TriggerIndex int

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
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	ControllerLifetimeCtx   context.Context
	JsonnetLibraryNamespace string

	controller controller.TypedController[Request]
	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	mapper     meta.RESTMapper

	cachesMux sync.RWMutex
	caches    map[types.NamespacedName]*instanceCache
}

type instanceCache struct {
	triggerCaches map[int]*definitionCache
	contextCaches map[int]*definitionCache

	watchConfigHash string

	stop func()
}

func (ci *instanceCache) Stop() {
	ci.stop()
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
	cache cache.Cache

	cacheReadyMux sync.Mutex
	cacheReady    error
}

func (ci *definitionCache) CacheReady() (bool, error) {
	ci.cacheReadyMux.Lock()
	defer ci.cacheReadyMux.Unlock()
	return ci.cacheReady == nil, ignoreErrCacheNotReady(ci.cacheReady)
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

//+kubebuilder:rbac:groups="",resources=serviceaccounts/token,verbs=create
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

type EspejoteErrorType string

const (
	ServiceAccountError          EspejoteErrorType = "ServiceAccountError"
	DependencyConfigurationError EspejoteErrorType = "DependencyConfigurationError"
	TemplateError                EspejoteErrorType = "TemplateError"
	ApplyError                   EspejoteErrorType = "ApplyError"
	TemplateReturnError          EspejoteErrorType = "TemplateReturnError"
)

type EspejoteError struct {
	error
	Type EspejoteErrorType
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

	res, recErr := r.reconcile(ctx, req)
	return res, multierr.Combine(recErr, r.recordReconcileErr(ctx, req.NamespacedName, recErr))
}

func (r *ManagedResourceReconciler) reconcile(ctx context.Context, req Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceReconciler.reconcile")
	l.Info("Reconciling ManagedResource")

	var managedResource espejotev1alpha1.ManagedResource
	if err := r.Get(ctx, req.NamespacedName, &managedResource); err != nil {
		if apierrors.IsNotFound(err) {
			r.stopAndRemoveCacheFor(managedResource)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
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
		return ctrl.Result{Requeue: true}, nil
	}

	jvm := jsonnet.MakeVM()
	jvm.Importer(&MultiImporter{
		Importers: []MultiImporterConfig{
			{
				Importer: &jsonnet.MemoryImporter{
					Data: map[string]jsonnet.Contents{
						"espejote.libsonnet": jsonnet.MakeContents(espejoteLibsonnet),
					},
				},
			},
			{
				TrimPathPrefix: "lib/",
				Importer: &ManifestImporter{
					Client:    r.Client,
					Namespace: r.JsonnetLibraryNamespace,
				},
			},
			{
				Importer: &ManifestImporter{
					Client:    r.Client,
					Namespace: managedResource.GetNamespace(),
				},
			},
		},
	})

	triggerJSON, err := r.renderTriggers(ctx, ci, req)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to render triggers: %w", err)
	}
	// Mark as internal so we can change the implementation later, to make it more efficient for example
	jvm.ExtCode("__internal_use_espejote_lib_trigger", triggerJSON)

	contextJSON, err := r.renderContexts(ctx, ci, managedResource)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to render contexts: %w", err)
	}
	jvm.ExtCode("__internal_use_espejote_lib_context", contextJSON)

	rendered, err := jvm.EvaluateAnonymousSnippet("template", managedResource.Spec.Template)
	if err != nil {
		return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to render template: %w", err), TemplateError)
	}
	rendered = strings.Trim(rendered, " \t\r\n")

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
	fieldValidation := managedResource.Spec.ApplyOptions.FieldValidation
	if fieldValidation == "" {
		fieldValidation = "Strict"
	} else {
		l.Info("using custom field validation", "validation", fieldValidation)
	}
	fieldOwner := managedResource.Spec.ApplyOptions.FieldManager
	if fieldOwner == "" {
		fieldOwner = fmt.Sprintf("managed-resource:%s", managedResource.GetName())
	}
	applyOpts := []client.PatchOption{client.FieldValidation(fieldValidation), client.FieldOwner(fieldOwner)}
	if managedResource.Spec.ApplyOptions.Force {
		applyOpts = append(applyOpts, client.ForceOwnership)
	}
	applyErrs := make([]error, 0, len(objects))
	for _, obj := range objects {
		err := r.Client.Patch(ctx, obj, client.Apply, applyOpts...)
		if err != nil {
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
func (r *ManagedResourceReconciler) recordReconcileErr(ctx context.Context, mr types.NamespacedName, recErr error) error {
	var managedResource espejotev1alpha1.ManagedResource
	if err := r.Get(ctx, mr, &managedResource); err != nil {
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
	var espejoteErr EspejoteError
	if errors.As(recErr, &espejoteErr) {
		errType = string(espejoteErr.Type)
	}

	r.Recorder.Eventf(&managedResource, "Warning", errType, "Reconcile error: %s", recErr.Error())

	if managedResource.Status.Status == errType {
		return nil
	}

	managedResource.Status.Status = errType
	return r.Status().Update(ctx, &managedResource)
}

var ErrCacheNotReady = errors.New("cache not ready")
var ErrFailedSyncCache = errors.New("failed to sync cache")

// stopAndRemoveCacheFor stops and removes the cache for the given ManagedResource.
func (r *ManagedResourceReconciler) stopAndRemoveCacheFor(mr espejotev1alpha1.ManagedResource) {
	k := client.ObjectKeyFromObject(&mr)

	r.cachesMux.RLock()
	_, ok := r.caches[k]
	if !ok {
		r.cachesMux.RUnlock()
		return
	}
	r.cachesMux.RUnlock()

	r.cachesMux.Lock()
	defer r.cachesMux.Unlock()

	ci, ok := r.caches[k]
	if !ok {
		return
	}
	ci.Stop()
	delete(r.caches, k)
}

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
	configHash := fmt.Sprintf("%x", hsh.Sum(nil))

	r.cachesMux.RLock()
	ci, ok := r.caches[k]
	if ok && ci.watchConfigHash == configHash {
		r.cachesMux.RUnlock()
		return ci, nil
	}
	r.cachesMux.RUnlock()

	r.cachesMux.Lock()
	defer r.cachesMux.Unlock()
	if ok && ci.watchConfigHash == configHash {
		return ci, nil
	} else if ok {
		l.Info("cache config changed, stopping old cache", "old", ci.watchConfigHash, "new", configHash)
		ci.Stop()
	}

	rc, err := r.restConfigForManagedResource(r.ControllerLifetimeCtx, mr)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for managed resource: %w", err)
	}

	cns := sets.New[string]()
	for _, con := range mr.Spec.Context {
		if cns.Has(con.Def) {
			return nil, fmt.Errorf("duplicate context definition %q", con.Def)
		}
		cns.Insert(con.Def)
	}

	cctx, cancel := context.WithCancel(r.ControllerLifetimeCtx)
	ci = &instanceCache{
		triggerCaches:   make(map[int]*definitionCache),
		contextCaches:   make(map[int]*definitionCache),
		stop:            cancel,
		watchConfigHash: configHash,
	}
	for ti, trigger := range mr.Spec.Triggers {
		if trigger.Interval.Duration != 0 {
			if err := r.setupIntervalTrigger(cctx, trigger.Interval.Duration, k, ti); err != nil {
				cancel()
				return nil, fmt.Errorf("failed to setup interval trigger %d: %w", ti, err)
			}
			continue
		}

		if trigger.WatchResource.APIVersion == "" {
			continue
		}

		watchTarget, dc, err := r.newCacheForResourceAndRESTClient(cctx, trigger.WatchResource, rc, mr.GetNamespace())
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create cache for trigger %d: %w", ti, err)
		}

		ci.triggerCaches[ti] = dc

		if err := r.controller.Watch(source.TypedKind(dc.cache, watchTarget, handler.TypedEnqueueRequestsFromMapFunc(staticMapFunc(client.ObjectKeyFromObject(&mr), ti)))); err != nil {
			cancel()
			return nil, err
		}
	}

	for conI, con := range mr.Spec.Context {
		if con.Resource.APIVersion == "" {
			continue
		}

		watchTarget, dc, err := r.newCacheForResourceAndRESTClient(cctx, con.Resource, rc, mr.GetNamespace())
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create cache for resource %d: %w", conI, err)
		}

		ci.contextCaches[conI] = dc

		// We only allow querying objects with existing informers
		if _, err := dc.cache.GetInformer(cctx, watchTarget); err != nil {
			cancel()
			return nil, err
		}
	}

	if r.caches == nil {
		r.caches = make(map[types.NamespacedName]*instanceCache)
	}
	r.caches[k] = ci
	return ci, nil
}

// Setup sets up the controller with the Manager.
func (r *ManagedResourceReconciler) Setup(cfg *rest.Config, mgr ctrl.Manager) error {
	c, err := builder.TypedControllerManagedBy[Request](mgr).
		Named("managed_resource").
		Watches(&espejotev1alpha1.ManagedResource{}, handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []Request {
			return []Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: a.GetNamespace(),
						Name:      a.GetName(),
					},
				},
			}
		})).
		Build(r)
	if err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}
	r.controller = c

	kubernetesClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	r.clientset = kubernetesClient
	r.restConfig = cfg
	r.mapper = mgr.GetRESTMapper()

	return err
}

// renderTriggers renders the trigger information for the given Request.
func (r *ManagedResourceReconciler) renderTriggers(ctx context.Context, ci *instanceCache, req Request) (string, error) {
	triggerInfo := struct {
		WatchResource encjson.RawMessage `json:"WatchResource,omitempty"`
	}{}

	if req.TriggerInfo.WatchResource != (WatchResource{}) {
		var triggerObj unstructured.Unstructured
		triggerObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   req.TriggerInfo.WatchResource.Group,
			Version: req.TriggerInfo.WatchResource.APIVersion,
			Kind:    req.TriggerInfo.WatchResource.Kind,
		})
		tc, ok := ci.triggerCaches[req.TriggerInfo.TriggerIndex]
		if !ok {
			// Should not happen as we checked all cache configuration before
			return "", fmt.Errorf("cache for trigger %d not found", req.TriggerInfo.TriggerIndex)
		}
		err := tc.cache.Get(ctx, types.NamespacedName{Namespace: req.TriggerInfo.WatchResource.Namespace, Name: req.TriggerInfo.WatchResource.Name}, &triggerObj)
		if err == nil {
			triggerJSONBytes, err := json.Marshal(triggerObj.UnstructuredContent())
			if err != nil {
				return "", fmt.Errorf("failed to marshal trigger info: %w", err)
			}
			triggerInfo.WatchResource = triggerJSONBytes
		} else if apierrors.IsNotFound(err) {
			log.FromContext(ctx).WithValues("trigger", req.TriggerInfo.WatchResource).Info("trigger object not found, was deleted")
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
func (r *ManagedResourceReconciler) renderContexts(ctx context.Context, ci *instanceCache, managedResource espejotev1alpha1.ManagedResource) (string, error) {
	contexts := map[string]any{}
	for i, con := range managedResource.Spec.Context {
		if con.Resource.APIVersion == "" {
			continue
		}
		cc, ok := ci.contextCaches[i]
		if !ok {
			// Should not happen as we checked all cache configuration before
			return "", fmt.Errorf("cache for context %d not found", i)
		}
		var contextObj unstructured.Unstructured
		contextObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   con.Resource.Group,
			Version: con.Resource.APIVersion,
			Kind:    con.Resource.Kind,
		})
		contextObjs, err := contextObj.ToList()
		if err != nil {
			return "", fmt.Errorf("failed to build context list: %w", err)
		}
		if err := cc.cache.List(ctx, contextObjs); err != nil {
			return "", fmt.Errorf("failed to list context objects: %w", err)
		}

		unwrapped := make([]any, len(contextObjs.Items))
		for i, obj := range contextObjs.Items {
			unwrapped[i] = obj.UnstructuredContent()
		}
		contexts[con.Def] = unwrapped
	}
	contextJSON, err := json.Marshal(contexts)
	if err != nil {
		return "", fmt.Errorf("failed to marshal contexts: %w", err)
	}
	return string(contextJSON), nil
}

// unpackRenderedObjects unpacks the rendered template into a list of client.Objects.
// The rendered template can be a single object, a list of objects or null.
func (r *ManagedResourceReconciler) unpackRenderedObjects(ctx context.Context, rendered string) ([]client.Object, error) {
	var objects []client.Object
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
func (r *ManagedResourceReconciler) newCacheForResourceAndRESTClient(ctx context.Context, cr espejotev1alpha1.ClusterResource, rc *rest.Config, defaultNamespace string) (client.Object, *definitionCache, error) {
	watchTarget := &unstructured.Unstructured{}
	watchTarget.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   cr.GetGroup(),
		Version: cr.GetAPIVersion(),
		Kind:    cr.GetKind(),
	})

	isNamespaced, err := apiutil.IsObjectNamespaced(watchTarget, r.Scheme, r.mapper)
	if err != nil {
		return watchTarget, nil, fmt.Errorf("failed to determine if watch target %q is namespaced: %w", cr, err)
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
			return watchTarget, nil, fmt.Errorf("failed to parse label selector for trigger %q: %w", cr, err)
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

	c, err := cache.New(rc, cache.Options{
		Scheme: r.Scheme,
		Mapper: r.mapper,
		// We want to explicitly fail if the informer is missing, otherwise we might create some unconfigured caches without any warning on programming errors.
		ReaderFailOnMissingInformer: true,
		DefaultFieldSelector:        fields.AndSelectors(sel...),
		DefaultLabelSelector:        lblSel,

		NewInformer: filterInf,
	})
	if err != nil {
		return watchTarget, nil, fmt.Errorf("failed to create cache for trigger %q: %w", cr, err)
	}

	dc := &definitionCache{
		cache:      c,
		cacheReady: ErrCacheNotReady,
	}

	go c.Start(ctx)
	go func(ctx context.Context, dc *definitionCache) {
		success := c.WaitForCacheSync(r.ControllerLifetimeCtx)
		dc.cacheReadyMux.Lock()
		defer dc.cacheReadyMux.Unlock()

		if success {
			dc.cacheReady = nil
		} else {
			dc.cacheReady = ErrFailedSyncCache
		}
	}(ctx, dc)

	return watchTarget, dc, nil
}

// setupIntervalTrigger sets up a trigger that fires every interval.
// The trigger is enqueued with the ManagedResource's key and the trigger index.
// The trigger is shut down when the context is canceled.
func (r *ManagedResourceReconciler) setupIntervalTrigger(ctx context.Context, interval time.Duration, mrKey types.NamespacedName, triggerIndex int) error {
	tick := time.NewTicker(interval)
	evChan := make(chan event.TypedGenericEvent[time.Time])
	go func() {
		for {
			select {
			case <-ctx.Done():
				tick.Stop()
				close(evChan)
				return
			case t := <-tick.C:
				select {
				case <-ctx.Done():
					tick.Stop()
					close(evChan)
					return
				case evChan <- event.TypedGenericEvent[time.Time]{Object: t}:
				}
			}
		}
	}()

	return r.controller.Watch(source.TypedChannel(evChan, handler.TypedEnqueueRequestsFromMapFunc(func(_ context.Context, _ time.Time) []Request {
		return []Request{{NamespacedName: mrKey, TriggerInfo: TriggerInfo{TriggerIndex: triggerIndex}}}
	})))
}

func staticMapFunc(r types.NamespacedName, triggerIndex int) func(context.Context, client.Object) []Request {
	return func(_ context.Context, o client.Object) []Request {
		gvk := o.GetObjectKind().GroupVersionKind()
		return []Request{
			{
				NamespacedName: r,
				TriggerInfo: TriggerInfo{
					TriggerIndex: triggerIndex,

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
