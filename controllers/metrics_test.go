package controllers_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

func Test_ManagedResourceStatusCollector(t *testing.T) {
	c := buildFakeClient(t,
		&espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "empty",
				Namespace: "default",
			},
		},
		&espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ready",
				Namespace: "default",
			},
			Status: espejotev1alpha1.ManagedResourceStatus{
				Status: "Ready",
			},
		},
		&espejotev1alpha1.ManagedResource{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "error",
				Namespace: "default",
			},
			Status: espejotev1alpha1.ManagedResourceStatus{
				Status: "DependencyConfigurationError",
			},
		},
	)

	subject := &controllers.ManagedResourceStatusCollector{
		Reader: c,
	}

	metrics := `
		# HELP espejote_managedresource_status Status of the managed resource. Read from the resources .status.status field.
		# TYPE espejote_managedresource_status gauge
		espejote_managedresource_status{managedresource="empty",namespace="default",status=""} 1
		espejote_managedresource_status{managedresource="error",namespace="default",status="DependencyConfigurationError"} 1
		espejote_managedresource_status{managedresource="ready",namespace="default",status="Ready"} 1
		# HELP espejote_managedresource_status_ready Ready status of the managed resource. 1 if ready, 0 if any other status. Read from the resources .status.status field.
		# TYPE espejote_managedresource_status_ready gauge
		espejote_managedresource_status_ready{managedresource="empty",namespace="default"} 0
		espejote_managedresource_status_ready{managedresource="error",namespace="default"} 0
		espejote_managedresource_status_ready{managedresource="ready",namespace="default"} 1
		`

	require.NoError(t,
		testutil.CollectAndCompare(subject, strings.NewReader(metrics), "espejote_managedresource_status", "espejote_managedresource_status_ready"),
	)
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
