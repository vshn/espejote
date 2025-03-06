package controllers

import (
	"context"
	encjson "encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/go-jsonnet"
	"go.uber.org/multierr"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

type Request = struct {
	NamespacedName types.NamespacedName
	// Include more trigger info
	TriggerInfo TriggerInfo
}

type TriggerInfo struct {
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

	cachesMux sync.RWMutex
	caches    map[types.NamespacedName]*cacheInfo
}

type cacheInfo struct {
	cache cache.Cache
	// resourceVersion string

	stop func()

	cacheReadyMux sync.Mutex
	cacheReady    error
}

func (ci *cacheInfo) Stop() {
	ci.stop()
}

func (ci *cacheInfo) CacheReady() (bool, error) {
	ci.cacheReadyMux.Lock()
	defer ci.cacheReadyMux.Unlock()
	return ci.cacheReady == nil, ci.cacheReady
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
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ci, err := r.cacheFor(managedResource)
	if err != nil {
		return ctrl.Result{}, err
	}
	ready, err := ci.CacheReady()
	if err != nil {
		return ctrl.Result{}, err
	}
	if !ready {
		// TODO: we could use a channel source to requeue when the cache is ready
		// not sure if worth the effort
		return ctrl.Result{RequeueAfter: 1}, nil
	}

	jvm := jsonnet.MakeVM()
	jvm.Importer(&ManifestImporter{
		Client:    r.Client,
		Namespace: r.JsonnetLibraryNamespace,
	})

	triggerJSON := `null`
	if req.TriggerInfo != (TriggerInfo{}) {
		var triggerObj unstructured.Unstructured
		triggerObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   req.TriggerInfo.WatchResource.Group,
			Version: req.TriggerInfo.WatchResource.APIVersion,
			Kind:    req.TriggerInfo.WatchResource.Kind,
		})
		if err := ci.cache.Get(ctx, types.NamespacedName{Namespace: managedResource.Namespace, Name: req.NamespacedName.Name}, &triggerObj); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get trigger object: %w", err)
		}
		triggerJSONBytes, err := json.Marshal(triggerObj.UnstructuredContent())
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to marshal trigger info: %w", err)
		}
		triggerJSON = string(triggerJSONBytes)
	}
	jvm.ExtCode("trigger", triggerJSON)

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
	} else if rendered == "null" {
		l.V(1).Info("rendered template returned null")
	} else {
		return ctrl.Result{}, fmt.Errorf("unexpected rendered template: %q", rendered)
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

func (r *ManagedResourceReconciler) cacheFor(mr espejotev1alpha1.ManagedResource) (*cacheInfo, error) {
	k := client.ObjectKeyFromObject(&mr)

	r.cachesMux.RLock()
	ci, ok := r.caches[k]
	r.cachesMux.RUnlock()

	if ok {
		return ci, nil
	}

	r.cachesMux.Lock()
	defer r.cachesMux.Unlock()
	ci, ok = r.caches[k]
	if ok {
		return ci, nil
	}

	rc, err := r.restConfigForManagedResource(r.ControllerLifetimeCtx, mr)
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config for managed resource: %w", err)
	}

	c, err := cache.New(rc, cache.Options{
		Scheme:                      r.Scheme,
		ReaderFailOnMissingInformer: true,
		DefaultNamespaces: map[string]cache.Config{
			mr.Namespace: {},
		},
	})
	if err != nil {
		return nil, err
	}

	cctx, cancel := context.WithCancel(r.ControllerLifetimeCtx)
	ci = &cacheInfo{
		cache:      c,
		stop:       cancel,
		cacheReady: ErrCacheNotReady,
	}
	go ci.cache.Start(cctx)
	go func(ctx context.Context, ci *cacheInfo) {
		success := c.WaitForCacheSync(r.ControllerLifetimeCtx)
		ci.cacheReadyMux.Lock()
		defer ci.cacheReadyMux.Unlock()

		if success {
			ci.cacheReady = nil
		} else {
			ci.cacheReady = ErrFailedSyncCache
		}
	}(cctx, ci)

	for _, trigger := range mr.Spec.Triggers {
		if trigger.WatchResource.APIVersion == "" {
			continue
		}

		watchTarget := &unstructured.Unstructured{}
		watchTarget.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   trigger.WatchResource.Group,
			Version: trigger.WatchResource.APIVersion,
			Kind:    trigger.WatchResource.Kind,
		})

		err = r.controller.Watch(source.TypedKind[client.Object](ci.cache, watchTarget, handler.TypedEnqueueRequestsFromMapFunc(staticMapFunc(client.ObjectKeyFromObject(&mr)))))
		if err != nil {
			cancel()
			return nil, err
		}
	}

	if r.caches == nil {
		r.caches = make(map[types.NamespacedName]*cacheInfo)
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

	return err
}

// jwtTokenForSA returns a JWT token for the given service account.
// The token is valid for 1 year.
func (r *ManagedResourceReconciler) jwtTokenForSA(ctx context.Context, namespace, name string) (string, error) {
	treq, err := r.clientset.CoreV1().ServiceAccounts(namespace).CreateToken(ctx, name, &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To(int64(60 * 60 * 24 * 365)), // 1 year
			BoundObjectRef:    &authv1.BoundObjectReference{},
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

	crc := r.restConfig
	config := rest.Config{
		Host:            crc.Host,
		BearerToken:     token,
		TLSClientConfig: *crc.TLSClientConfig.DeepCopy(),
	}
	return &config, nil
}

func staticMapFunc(r types.NamespacedName) func(context.Context, client.Object) []Request {
	return func(_ context.Context, o client.Object) []Request {
		gvk := o.GetObjectKind().GroupVersionKind()
		return []Request{
			{
				NamespacedName: r,
				TriggerInfo: TriggerInfo{
					WatchResource{
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
