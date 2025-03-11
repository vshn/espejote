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

func (r *ManagedResourceReconciler) Reconcile(ctx context.Context, req Request) (ctrl.Result, error) {
	l := log.FromContext(ctx).WithName("ManagedResourceReconciler.Reconcile").WithValues("request", req)
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
		return ctrl.Result{}, err
	}
	ready, err := ci.AllCachesReady()
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ready {
		return ctrl.Result{Requeue: true}, nil
	}

	jvm := jsonnet.MakeVM()
	jvm.Importer(&MultiImporter{
		Importers: []jsonnet.Importer{
			&jsonnet.MemoryImporter{
				Data: map[string]jsonnet.Contents{
					"espejote.libsonnet": jsonnet.MakeContents(espejoteLibsonnet),
				},
			},
			&ManifestImporter{
				Client:    r.Client,
				Namespace: r.JsonnetLibraryNamespace,
			},
		},
	})

	triggerJSON := jsonNull
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
			return ctrl.Result{}, fmt.Errorf("cache for trigger %d not found", req.TriggerInfo.TriggerIndex)
		}
		err := tc.cache.Get(ctx, types.NamespacedName{Namespace: req.TriggerInfo.WatchResource.Namespace, Name: req.TriggerInfo.WatchResource.Name}, &triggerObj)
		if err == nil {
			triggerJSONBytes, err := json.Marshal(triggerObj.UnstructuredContent())
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to marshal trigger info: %w", err)
			}
			triggerJSON = string(triggerJSONBytes)
		} else if apierrors.IsNotFound(err) {
			l.Info("trigger object not found, was deleted", "trigger", req.TriggerInfo.WatchResource)
		} else {
			return ctrl.Result{}, fmt.Errorf("failed to get trigger object: %w", err)
		}
	}
	jvm.ExtCode("__internal_use_espejote_lib_trigger", triggerJSON)

	contexts := map[string]any{}
	for i, con := range managedResource.Spec.Context {
		if con.Resource.APIVersion == "" {
			continue
		}
		cc, ok := ci.contextCaches[i]
		if !ok {
			// Should not happen as we checked all cache configuration before
			return ctrl.Result{}, fmt.Errorf("cache for context %d not found", i)
		}
		var contextObj unstructured.Unstructured
		contextObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   con.Resource.Group,
			Version: con.Resource.APIVersion,
			Kind:    con.Resource.Kind,
		})
		contextObjs, err := contextObj.ToList()
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to build context list: %w", err)
		}
		if err := cc.cache.List(ctx, contextObjs); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list context objects: %w", err)
		}

		unwrapped := make([]any, len(contextObjs.Items))
		for i, obj := range contextObjs.Items {
			unwrapped[i] = obj.UnstructuredContent()
		}
		contexts[con.Def] = unwrapped
	}
	contextJSON, err := json.Marshal(contexts)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to marshal contexts: %w", err)
	}
	jvm.ExtCode("__internal_use_espejote_lib_context", string(contextJSON))

	rendered, err := jvm.EvaluateAnonymousSnippet("template", managedResource.Spec.Template)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to render template: %w", err)
	}
	rendered = strings.Trim(rendered, " \t\r\n")

	var objects []client.Object
	if rendered != "" && strings.HasPrefix(rendered, "{") {
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON([]byte(rendered)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to unmarshal rendered template: %w", err)
		}
		objects = append(objects, obj)
	} else if rendered != "" && strings.HasPrefix(rendered, "[") {
		// RawMessage is used to delay unmarshaling
		var list []encjson.RawMessage
		if err := json.Unmarshal([]byte(rendered), &list); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to unmarshal rendered template: %w", err)
		}
		for _, raw := range list {
			obj := &unstructured.Unstructured{}
			if err := obj.UnmarshalJSON(raw); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to unmarshal rendered template: %w", err)
			}
			objects = append(objects, obj)
		}
	} else if rendered == jsonNull {
		l.V(1).Info("rendered template returned null")
	} else {
		return ctrl.Result{}, fmt.Errorf("unexpected rendered template: %q", rendered)
	}

	for _, obj := range objects {
		namespaced, err := apiutil.IsObjectNamespaced(obj, r.Scheme, r.mapper)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to determine if object is namespaced: %w", err)
		}
		if namespaced && obj.GetNamespace() == "" {
			obj.SetNamespace(managedResource.GetNamespace())
		}
	}

	applyErrs := make([]error, 0, len(objects))
	for _, obj := range objects {
		err := r.Client.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner("managed-resource-controller"))
		if err != nil {
			applyErrs = append(applyErrs, fmt.Errorf("failed to apply object %q %q: %w", obj.GetObjectKind(), obj.GetName(), err))
		}
	}
	if err := multierr.Combine(applyErrs...); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

var ErrCacheNotReady = errors.New("cache not ready")
var ErrFailedSyncCache = errors.New("failed to sync cache")

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

	cctx, cancel := context.WithCancel(r.ControllerLifetimeCtx)
	ci = &instanceCache{
		triggerCaches:   make(map[int]*definitionCache),
		contextCaches:   make(map[int]*definitionCache),
		stop:            cancel,
		watchConfigHash: configHash,
	}
	for ti, trigger := range mr.Spec.Triggers {
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

// jwtTokenForSA returns a JWT token for the given service account.
// The token is valid for 1 year.
func (r *ManagedResourceReconciler) jwtTokenForSA(ctx context.Context, namespace, name string) (string, error) {
	treq, err := r.clientset.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, name, &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To(int64(60 * 60 * 24 * 365)), // 1 year
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("token request for %q failed: %w", strings.Join([]string{"system:serviceaccount", namespace, name}, ":"), err)
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
					fmt.Println("!!!!!!!!!!!!! EVENT !!!!!!!!!!!!!!!", in.Type, obj.(*unstructured.Unstructured).UnstructuredContent())
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
