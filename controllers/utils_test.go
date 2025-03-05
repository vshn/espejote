package controllers

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// setupEnvtestEnv sets up a test environment for the controller tests.
// Registers a cleanup function to stop the test environment when the test ends.
// Any managers should be stopped before the test ends by registering a cleanup function or by cancelling the context.
// Returns the scheme and the config.
func setupEnvtestEnv(t *testing.T) (*runtime.Scheme, *rest.Config) {
	t.Helper()

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, espejotev1alpha1.AddToScheme(scheme))

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
		Scheme:                scheme,
	}

	cfg, err := testEnv.Start()
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, testEnv.Stop())
	})

	return scheme, cfg
}
