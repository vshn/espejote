package testutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// SetupEnvtestEnv sets up a test environment for the controller tests.
// Registers a cleanup function to stop the test environment when the test ends.
// Any managers should be stopped before the test ends by registering a cleanup function or by cancelling the context.
// Returns the scheme and the config.
func SetupEnvtestEnv(t *testing.T) (*runtime.Scheme, *rest.Config) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, espejotev1alpha1.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
		Scheme:                scheme,
		BinaryAssetsDirectory: getFirstFoundEnvTestBinaryDir(t),
	}

	cfg, err := testEnv.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, testEnv.Stop())
	})

	return scheme, cfg
}

// TmpNamespace creates a new namespace, with default service account, with a generated name and registers a cleanup function to delete it.
func TmpNamespace(t *testing.T, c client.Client) string {
	t.Helper()

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "espejote-test-",
			Annotations: map[string]string{
				"test.espejote.vshn.net/name": t.Name(),
			},
		},
	}
	require.NoError(t, c.Create(t.Context(), ns))

	defaultSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: ns.Name,
		},
	}
	require.NoError(t, c.Create(t.Context(), defaultSA))

	t.Log("Created namespace", ns.Name)

	t.Cleanup(func() {
		require.NoError(t, c.Delete(context.Background(), ns))
	})
	return ns.Name
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Makefile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'make test' once beforehand.
func getFirstFoundEnvTestBinaryDir(t *testing.T) string {
	basePath := filepath.Join("..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		t.Logf("Failed to read directory %q: %s", basePath, err.Error())
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
