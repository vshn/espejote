package controllers

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/DmitriyVTitov/size"
	"github.com/google/go-jsonnet"
	"github.com/google/go-jsonnet/ast"
	"go.uber.org/multierr"
	authv1 "k8s.io/api/authentication/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers/applygroup"
)

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
	Recorder events.EventRecorder

	ControllerLifetimeCtx   context.Context
	JsonnetLibraryNamespace string

	clientset  *kubernetes.Clientset
	restConfig *rest.Config
	mapper     meta.RESTMapper

	cache *instanceCache

	configHash       string
	configGeneration int64

	// started is set to true after the first reconcile has been started.
	// It's a bit of a hack to detect that the caches are synced as it's much easier than starting to track the cache sync state.
	started atomic.Bool
}

type instanceCache struct {
	triggerCaches map[string]*definitionCache
	contextCaches map[string]*definitionCache
}

var errCacheNotFound = errors.New("cache not found")

// clientForTrigger returns a client.Reader for the given trigger.
func (ci *instanceCache) clientForTrigger(triggerName string) (client.Reader, error) {
	dc, ok := ci.triggerCaches[triggerName]
	if !ok {
		return nil, fmt.Errorf("error getting cache for trigger %q: %w", triggerName, errCacheNotFound)
	}
	return dc.cache, nil
}

// clientForContext returns a client.Reader for the given context.
func (ci *instanceCache) clientForContext(contextName string) (client.Reader, error) {
	dc, ok := ci.contextCaches[contextName]
	if !ok {
		return nil, fmt.Errorf("error getting cache for context %q: %w", contextName, errCacheNotFound)
	}
	return dc.cache, nil
}

type definitionCache struct {
	target *unstructured.Unstructured

	cache cache.Cache
}

// Size returns the number of objects and the size of the cache in bytes.
// The size is an approximation and may not be accurate.
// The size is calculated by listing all objects in the cache and recursively adding the reflect.Size of each object.
// The assumption is that the objects are the biggest part of the cache.
// TODO: size.Of does a bunch of reflect and some allocations, we might want to simplify/optimize this. I never benchmarked it.
func (ci *definitionCache) Size(ctx context.Context) (int, int, error) {
	list, err := ci.target.DeepCopy().ToList()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build list from target object: %w", err)
	}
	if err := ci.cache.List(ctx, list); err != nil {
		return 0, 0, fmt.Errorf("failed to list objects: %w", err)
	}

	return len(list.Items), size.Of(list), nil
}

//+kubebuilder:rbac:groups=espejote.io,resources=managedresources,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=espejote.io,resources=managedresources/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=espejote.io,resources=managedresources/finalizers,verbs=update

//+kubebuilder:rbac:groups=espejote.io,resources=jsonnetlibraries,verbs=get;list;watch

//+kubebuilder:rbac:groups="",resources=serviceaccounts/token,verbs=create
//+kubebuilder:rbac:groups="";events.k8s.io,resources=events,verbs=create;update;patch

type EspejoteErrorType string

const (
	ServiceAccountError          EspejoteErrorType = "ServiceAccountError"
	WaitingForCacheSync          EspejoteErrorType = "WaitingForCacheSync"
	DependencyConfigurationError EspejoteErrorType = "DependencyConfigurationError"
	TemplateError                EspejoteErrorType = "TemplateError"
	ApplyError                   EspejoteErrorType = "ApplyError"
	TemplateReturnError          EspejoteErrorType = "TemplateReturnError"
	ControllerInstantiationError EspejoteErrorType = "ControllerInstantiationError"
	ControllerStartError         EspejoteErrorType = "ControllerStartError"
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
	r.started.CompareAndSwap(false, true)

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

	if r.configGeneration != managedResource.Generation {
		hsh, err := configHashForManagedResource(managedResource)
		if err != nil {
			return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to compute config hash: %w", err), DependencyConfigurationError)
		}
		if r.configHash != hsh {
			l.Info("Configuration changed, skipping reconciliation until this instance is stopped", "oldHash", r.configHash, "newHash", hsh)
			return ctrl.Result{}, nil
		}
		r.configGeneration = managedResource.Generation
	}

	rendered, err := (&Renderer{
		Importer:            FromClientImporter(r.Client, managedResource.GetNamespace(), r.JsonnetLibraryNamespace),
		TriggerClientGetter: r.cache.clientForTrigger,
		ContextClientGetter: r.cache.clientForContext,
	}).Render(ctx, managedResource, req.TriggerInfo)
	if err != nil {
		return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to render template: %w", err), TemplateError)
	}

	applier := applygroup.Applier{
		ApplyDefaults: applygroup.ApplyDefaults{
			ApplyOptions:         managedResource.Spec.ApplyOptions,
			FieldManagerFallback: fmt.Sprintf("managed-resource:%s", managedResource.GetName()),
		},
	}

	if err := json.Unmarshal([]byte(rendered), &applier); err != nil {
		return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to unmarshal rendered template: %w", err), TemplateReturnError)
	}

	counts := map[string]int{}
	if err := applier.Walk(func(a *applygroup.Applier) error {
		counts[a.Kind.String()]++

		if a.Resource == nil {
			return nil
		}
		return r.defaultNamespaceIfNamespaced(a.Resource, managedResource.GetNamespace())
	}); err != nil {
		return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to process rendered template: %w", err), ApplyError)
	}
	l.Info("Applying rendered objects", "kinds", counts)

	// apply objects returned by the template
	c, err := r.uncachedClientForManagedResource(ctx, managedResource)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get client for managed resource: %w", err)
	}

	if err := applier.Apply(ctx, c); err != nil {
		return ctrl.Result{}, newEspejoteError(fmt.Errorf("failed to apply objects: %w", err), ApplyError)
	}

	return ctrl.Result{}, nil
}

func (r *ManagedResourceReconciler) defaultNamespaceIfNamespaced(obj client.Object, namespace string) error {
	namespaced, err := apiutil.IsObjectNamespaced(obj, r.Scheme, r.mapper)
	if err != nil {
		return newEspejoteError(fmt.Errorf("failed to determine if object is namespaced: %w", err), ApplyError)
	}
	if namespaced && obj.GetNamespace() == "" {
		obj.SetNamespace(namespace)
	}
	return nil
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
		objs := []runtime.Object{&managedResource}
		if req.TriggerInfo.WatchResource != (WatchResource{}) {
			tobj := new(unstructured.Unstructured)
			tobj.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   req.TriggerInfo.WatchResource.Group,
				Version: req.TriggerInfo.WatchResource.APIVersion,
				Kind:    req.TriggerInfo.WatchResource.Kind,
			})
			tobj.SetName(req.TriggerInfo.WatchResource.Name)
			tobj.SetNamespace(req.TriggerInfo.WatchResource.Namespace)
			objs = append(objs, tobj)
		}
		for _, obj := range objs {
			r.Recorder.Eventf(obj, nil, "Warning", errType, "Reconcile", "Reconcile error: %s", recErr.Error())
		}
		reconcileErrors.WithLabelValues(r.For.Name, r.For.Namespace, req.TriggerInfo.TriggerName, errType).Inc()
	}

	if managedResource.Status.Status == errType {
		return nil
	}

	managedResource.Status.Status = errType
	return r.Status().Update(ctx, &managedResource)
}

// Render renders the given ManagedResource.
type Renderer struct {
	Importer jsonnet.Importer

	TriggerClientGetter func(triggerName string) (client.Reader, error)
	ContextClientGetter func(contextName string) (client.Reader, error)
}

// Render renders the given ManagedResource.
func (r *Renderer) Render(ctx context.Context, managedResource espejotev1alpha1.ManagedResource, ti TriggerInfo) (string, error) {
	jvm := jsonnet.MakeVM()
	jvm.Importer(r.Importer)

	triggerNode, err := r.renderTriggers(ctx, r.TriggerClientGetter, ti)
	if err != nil {
		return "", fmt.Errorf("failed to render triggers: %w", err)
	}
	// Mark as internal so we can change the implementation later, to make it more efficient for example
	jvm.ExtNode("__internal_use_espejote_lib_trigger", triggerNode)

	contextNode, err := r.renderContexts(ctx, r.ContextClientGetter, managedResource)
	if err != nil {
		return "", fmt.Errorf("failed to render contexts: %w", err)
	}
	jvm.ExtNode("__internal_use_espejote_lib_context", contextNode)

	rendered, err := jvm.EvaluateAnonymousSnippet("template", managedResource.Spec.Template)
	if err != nil {
		return "", fmt.Errorf("failed to render template: %w", err)
	}
	return rendered, nil
}

// renderTriggers renders the trigger information for the given Request.
func (r *Renderer) renderTriggers(ctx context.Context, getReader func(string) (client.Reader, error), ti TriggerInfo) (ast.Node, error) {
	triggerInfo := &ast.DesugaredObject{}

	if ti.TriggerName != "" {
		triggerInfo.Fields = append(triggerInfo.Fields, jsonnetObjectField("name", jsonnetString(ti.TriggerName)))
	}

	if ti.WatchResource != (WatchResource{}) {
		triggerData := &ast.DesugaredObject{
			Fields: []ast.DesugaredObjectField{
				jsonnetObjectField("resourceEvent", &ast.DesugaredObject{
					Fields: []ast.DesugaredObjectField{
						jsonnetObjectField("apiVersion", jsonnetString(schema.GroupVersion{Group: ti.WatchResource.Group, Version: ti.WatchResource.APIVersion}.String())),
						jsonnetObjectField("kind", jsonnetString(ti.WatchResource.Kind)),
						jsonnetObjectField("name", jsonnetString(ti.WatchResource.Name)),
						jsonnetObjectField("namespace", jsonnetString(ti.WatchResource.Namespace)),
					},
				}),
			},
		}

		var triggerObj unstructured.Unstructured
		triggerObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   ti.WatchResource.Group,
			Version: ti.WatchResource.APIVersion,
			Kind:    ti.WatchResource.Kind,
		})

		resourceNode := ast.Node(&ast.LiteralNull{})
		reader, err := getReader(ti.TriggerName)
		if err == nil {
			err = reader.Get(ctx, types.NamespacedName{Namespace: ti.WatchResource.Namespace, Name: ti.WatchResource.Name}, &triggerObj)
			if err == nil {
				node, err := jsonValueToJsonnetNode(triggerObj.UnstructuredContent())
				if err != nil {
					return nil, fmt.Errorf("failed to marshal trigger object: %w", err)
				}
				resourceNode = node
			} else if apierrors.IsNotFound(err) {
				log.FromContext(ctx).WithValues("trigger", ti.WatchResource).Info("trigger object not found, was deleted")
			} else {
				return nil, fmt.Errorf("failed to get trigger object: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to get reader for trigger: %w", err)
		}

		triggerData.Fields = append(triggerData.Fields, jsonnetObjectField("resource", resourceNode))
		triggerInfo.Fields = append(triggerInfo.Fields, jsonnetObjectField("data", triggerData))
	}

	return triggerInfo, nil
}

// renderContexts renders the context information for the given ManagedResource.
func (r *Renderer) renderContexts(ctx context.Context, getReader func(string) (client.Reader, error), managedResource espejotev1alpha1.ManagedResource) (ast.Node, error) {
	contexts := map[string]any{}
	for _, con := range managedResource.Spec.Context {
		if con.Resource.APIVersion == "" {
			continue
		}
		reader, err := getReader(con.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get reader for context: %w", err)
		}
		var contextObj unstructured.Unstructured
		contextObj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   con.Resource.GetGroup(),
			Version: con.Resource.GetVersion(),
			Kind:    con.Resource.GetKind(),
		})
		contextObjs, err := contextObj.ToList()
		if err != nil {
			return nil, fmt.Errorf("failed to build context list: %w", err)
		}
		if err := reader.List(ctx, contextObjs); err != nil {
			return nil, fmt.Errorf("failed to list context objects: %w", err)
		}

		unwrapped := make([]any, len(contextObjs.Items))
		for i, obj := range contextObjs.Items {
			unwrapped[i] = obj.UnstructuredContent()
		}
		contexts[con.Name] = unwrapped
	}
	contextNode, err := jsonValueToJsonnetNode(contexts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal contexts: %w", err)
	}
	return contextNode, nil
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

func jsonValueToJsonnetNode(v any) (ast.Node, error) {
	if v == nil {
		return &ast.LiteralNull{}, nil
	}

	switch val := v.(type) {
	case ast.Node:
		return val, nil
	case bool:
		return &ast.LiteralBoolean{Value: val}, nil
	case int64, float64:
		jsonVal, err := json.Marshal(val)
		if err != nil {
			return nil, err
		}
		return &ast.LiteralNumber{OriginalString: string(jsonVal)}, nil
	case string:
		return jsonnetString(val), nil
	case []any:
		elems := make([]ast.CommaSeparatedExpr, len(val))
		for i, e := range val {
			n, err := jsonValueToJsonnetNode(e)
			if err != nil {
				return nil, err
			}
			elems[i] = ast.CommaSeparatedExpr{Expr: n}
		}
		return &ast.Array{Elements: elems}, nil
	case map[string]any:
		fields := make(ast.DesugaredObjectFields, 0, len(val))
		for k, v := range val {
			n, err := jsonValueToJsonnetNode(v)
			if err != nil {
				return nil, err
			}
			fields = append(fields, jsonnetObjectField(k, n))
		}
		return &ast.DesugaredObject{Fields: fields}, nil
	default:
		return nil, fmt.Errorf("unsupported json value type: %T", v)
	}
}

func jsonnetString(s string) ast.Node {
	return &ast.LiteralString{Value: s, Kind: ast.StringDouble}
}

func jsonnetObjectField(name string, body ast.Node) ast.DesugaredObjectField {
	return ast.DesugaredObjectField{
		Name: jsonnetString(name),
		Body: body,
		Hide: ast.ObjectFieldInherit,
	}
}
