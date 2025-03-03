package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_ManagedResourceReconciler_Reconcile(t *testing.T) {
	scheme, cfg := setupEnvtestEnv(t)
	c, err := client.NewWithWatch(cfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	t.Run("basic reconciliation", func(t *testing.T) {
		subject := &ManagedResourceReconciler{
			Client: c,
			Scheme: c.Scheme(),
		}
		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: scheme,
		})
		require.NoError(t, err)

		require.NoError(t, subject.SetupWithManager(mgr))

		mgrCtx, mgrCancel := context.WithCancel(t.Context())
		t.Cleanup(mgrCancel)
		go mgr.Start(mgrCtx)
	})
}
