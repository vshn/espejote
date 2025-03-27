package admission

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-jsonnet"
	"github.com/prometheus/client_golang/prometheus"
	"gomodules.xyz/jsonpatch/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

var (
	admissionRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: controllers.MetricsNamespace,
			Name:      "admission_requests_total",
			Help:      "Total number of reconciles by trigger.",
		},
		[]string{"admission", "namespace", "code"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		admissionRequestsTotal,
	)
}

//+kubebuilder:rbac:groups=espejote.io,resources=jsonnetlibraries,verbs=get;list;watch

type pathContextKey struct{}

func withNamespaceName(ctx context.Context, nsn types.NamespacedName) context.Context {
	return context.WithValue(ctx, pathContextKey{}, nsn)
}

func namespaceNameFrom(ctx context.Context) types.NamespacedName {
	if nsn, ok := ctx.Value(pathContextKey{}).(types.NamespacedName); ok {
		return nsn
	}
	return types.NamespacedName{}
}

// NewHandler returns a new admission handler for the Admission resource.
// It expects to be registered with the webhook server.
// The URL must include {namespace} and {name} path parameters as they are used to fetch the Admission resource.
func NewHandler(c client.Client, jsonnetLibraryNamespace string) *webhook.Admission {
	return &webhook.Admission{
		Handler: &handler{
			Client:                  c,
			JsonnetLibraryNamespace: jsonnetLibraryNamespace,
		},
		WithContextFunc: func(ctx context.Context, req *http.Request) context.Context {
			nsn := types.NamespacedName{
				Namespace: req.PathValue("namespace"),
				Name:      req.PathValue("name"),
			}
			return withNamespaceName(ctx, nsn)
		},
	}
}

type handler struct {
	Client client.Client

	JsonnetLibraryNamespace string
}

func (h *handler) Handle(ctx context.Context, req webhook.AdmissionRequest) webhook.AdmissionResponse {
	admissionKey := namespaceNameFrom(ctx)
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues("admission", admissionKey))
	l := log.FromContext(ctx).WithName("admission.handler.Handle")

	ret := h.handle(ctx, admissionKey, req)

	msg := "<not given>"
	status := http.StatusOK
	if ret.Result != nil {
		msg = ret.Result.Message
		status = int(ret.Result.Code)
	}

	admissionRequestsTotal.WithLabelValues(admissionKey.Namespace, admissionKey.Name, fmt.Sprint(status)).Inc()
	l.Info("Admission response", "allowed", ret.Allowed, "message", msg, "status", status, "patch", fmt.Sprintf("%s", ret.Patches))
	return ret
}

func (h *handler) handle(ctx context.Context, admissionKey types.NamespacedName, req webhook.AdmissionRequest) webhook.AdmissionResponse {
	if admissionKey == (types.NamespacedName{}) {
		return admission.Errored(http.StatusBadRequest, errors.New("missing namespace and name in context"))
	}

	var adm espejotev1alpha1.Admission
	if err := h.Client.Get(ctx, admissionKey, &adm); err != nil {
		if apierrors.IsNotFound(err) {
			return admission.Allowed("Admission not found")
		}
		return admission.Errored(http.StatusInternalServerError, err)
	}

	jvm := jsonnet.MakeVM()
	jvm.Importer(controllers.FromClientImporter(h.Client, adm.GetNamespace(), h.JsonnetLibraryNamespace))

	reqJson, err := json.Marshal(req)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to marshal request: %w", err))
	}
	jvm.ExtCode("__internal_use_espejote_lib_admissionrequest", string(reqJson))

	ret, err := jvm.EvaluateAnonymousSnippet("admission", adm.Spec.Template)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to evaluate jsonnet: %w", err))
	}

	var resp admissionResponse
	if err := json.Unmarshal([]byte(ret), &resp); err != nil {
		return admission.Errored(http.StatusInternalServerError, fmt.Errorf("failed to unmarshal response: %w", err))
	}

	return resp.toAdmissionResponse()
}

type admissionResponse struct {
	Allowed bool                  `json:"allowed"`
	Message string                `json:"message,omitempty"`
	Patches []jsonpatch.Operation `json:"patches,omitempty"`
}

func (ar admissionResponse) toAdmissionResponse() webhook.AdmissionResponse {
	war := admission.ValidationResponse(ar.Allowed, ar.Message)
	war.Patches = append(war.Patches, ar.Patches...)
	return war
}
