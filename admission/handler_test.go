package admission_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espadmission "github.com/vshn/espejote/admission"
	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

func Test_Handler_AdmissionNotFound(t *testing.T) {
	t.Parallel()

	c := buildFakeClient(t)

	subject := espadmission.NewHandler(c, "default")
	require.NotNil(t, subject)

	w := httptest.NewRecorder()
	req := newAdmissionRequest(t, "test", "default", admissionv1.AdmissionRequest{
		UID: "test",
	})
	subject.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	require.NotNil(t, res.Body)
	defer res.Body.Close()
	var admres admissionv1.AdmissionReview
	require.NoError(t, json.NewDecoder(res.Body).Decode(&admres))
	require.NotNil(t, admres.Response)
	require.NotNil(t, admres.Response.Result)
	assert.Equal(t, http.StatusOK, int(admres.Response.Result.Code))
	assert.Equal(t, "Admission not found", admres.Response.Result.Message)
}

func Test_Handler_Allowed(t *testing.T) {
	t.Parallel()

	c := buildFakeClient(t, &espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Template: `
			local esp = import 'espejote.libsonnet';

			esp.ALPHA.admission.allowed("Nice job!")
`,
		},
	})

	subject := espadmission.NewHandler(c, "default")
	require.NotNil(t, subject)

	w := httptest.NewRecorder()
	req := newAdmissionRequest(t, "test", "default", admissionv1.AdmissionRequest{
		UID: "test",
	})
	subject.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	require.NotNil(t, res.Body)
	defer res.Body.Close()
	var admres admissionv1.AdmissionReview
	require.NoError(t, json.NewDecoder(res.Body).Decode(&admres))
	require.NotNil(t, admres.Response)
	require.NotNil(t, admres.Response.Result)
	assert.Equal(t, http.StatusOK, int(admres.Response.Result.Code))
	assert.Equal(t, "Nice job!", admres.Response.Result.Message)
}

func Test_Handler_Denied(t *testing.T) {
	t.Parallel()

	c := buildFakeClient(t, &espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Template: `
			local esp = import 'espejote.libsonnet';

			esp.ALPHA.admission.denied("sod off")
`,
		},
	})

	subject := espadmission.NewHandler(c, "default")
	require.NotNil(t, subject)

	w := httptest.NewRecorder()
	req := newAdmissionRequest(t, "test", "default", admissionv1.AdmissionRequest{
		UID: "test",
	})
	subject.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	require.NotNil(t, res.Body)
	defer res.Body.Close()
	var admres admissionv1.AdmissionReview
	require.NoError(t, json.NewDecoder(res.Body).Decode(&admres))
	require.NotNil(t, admres.Response)
	require.NotNil(t, admres.Response.Result)
	assert.Equal(t, http.StatusForbidden, int(admres.Response.Result.Code))
	assert.Equal(t, "sod off", admres.Response.Result.Message)
}

func Test_Handler_Patched(t *testing.T) {
	t.Parallel()

	c := buildFakeClient(t, &espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Template: `
  local esp = import 'espejote.libsonnet';
  local admission = esp.ALPHA.admission;

  local user = admission.admissionRequest().userInfo.username;

  admission.patched("get patched", admission.assertPatch([
    admission.jsonPatchOp("add", "/metadata/annotations/request-user", user),
  ]))
`,
		},
	})

	subject := espadmission.NewHandler(c, "default")
	require.NotNil(t, subject)

	w := httptest.NewRecorder()
	req := newAdmissionRequest(t, "test", "default", admissionv1.AdmissionRequest{
		UID: "test",
		UserInfo: authenticationv1.UserInfo{
			Username: "testuser",
		},
		Object: objectToRaw(t, &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
				Annotations: map[string]string{
					"test": "test",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(3)),
			},
		}),
	})
	subject.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	require.NotNil(t, res.Body)
	defer res.Body.Close()
	var admres admissionv1.AdmissionReview
	require.NoError(t, json.NewDecoder(res.Body).Decode(&admres))
	require.NotNil(t, admres.Response)
	require.NotNil(t, admres.Response.Result)
	assert.Equal(t, http.StatusOK, int(admres.Response.Result.Code))
	assert.Equal(t, "get patched", admres.Response.Result.Message)
	assert.JSONEq(t, `[{"op":"add","path":"/metadata/annotations/request-user","value":"testuser"}]`, string(admres.Response.Patch))
}

func Test_Handler_Patched_InvalidPatch(t *testing.T) {
	t.Parallel()

	c := buildFakeClient(t, &espejotev1alpha1.Admission{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: espejotev1alpha1.AdmissionSpec{
			Template: `
  local esp = import 'espejote.libsonnet';
  local admission = esp.ALPHA.admission;

  local user = admission.admissionRequest().userInfo.username;

  admission.patched("get patched", admission.assertPatch([
    admission.jsonPatchOp("add", "/metadata/annotations/request-user", user),
  ]))
`,
		},
	})

	subject := espadmission.NewHandler(c, "default")
	require.NotNil(t, subject)

	w := httptest.NewRecorder()
	req := newAdmissionRequest(t, "test", "default", admissionv1.AdmissionRequest{
		UID: "test",
		UserInfo: authenticationv1.UserInfo{
			Username: "testuser",
		},
		Object: objectToRaw(t, &corev1.Namespace{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Namespace",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}),
	})
	subject.ServeHTTP(w, req)
	res := w.Result()
	require.Equal(t, http.StatusOK, res.StatusCode)

	require.NotNil(t, res.Body)
	defer res.Body.Close()
	var admres admissionv1.AdmissionReview
	require.NoError(t, json.NewDecoder(res.Body).Decode(&admres))
	require.NotNil(t, admres.Response)
	require.NotNil(t, admres.Response.Result)
	assert.Equal(t, http.StatusInternalServerError, int(admres.Response.Result.Code))
	assert.Contains(t, admres.Response.Result.Message, "add operation does not apply")
	assert.Contains(t, admres.Response.Result.Message, "doc is missing path")
	assert.Contains(t, admres.Response.Result.Message, "/metadata/annotations/request-user")
}

func newAdmissionRequest(t *testing.T, name, namespace string, admreq admissionv1.AdmissionRequest) *http.Request {
	t.Helper()

	b := admissionv1.AdmissionReview{
		Request: &admreq,
	}
	b.SetGroupVersionKind(admissionv1.SchemeGroupVersion.WithKind("AdmissionReview"))

	body := new(bytes.Buffer)
	require.NoError(t, json.NewEncoder(body).Encode(b))
	req := httptest.NewRequest("GET", path.Join("/dynamic", namespace, name), body)
	req = req.WithContext(log.IntoContext(req.Context(), testr.New(t)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("name", name)
	req.SetPathValue("namespace", namespace)
	return req
}

func objectToRaw(t *testing.T, obj client.Object) runtime.RawExtension {
	t.Helper()

	raw, err := json.Marshal(obj)
	require.NoError(t, err)
	return runtime.RawExtension{Raw: raw}
}

func buildFakeClient(t *testing.T, objs ...client.Object) client.WithWatch {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, espejotev1alpha1.AddToScheme(scheme))

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}
