package cmd

import (
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
	//+kubebuilder:scaffold:imports
)

var metricsAddr string
var enableLeaderElection bool
var probeAddr string
var zapOpts = zap.Options{
	Development: true,
}

func init() {
	rootCmd.AddCommand(controllerCmd)

	zapFlagSet := flag.NewFlagSet("zap", flag.ExitOnError)
	zapOpts.BindFlags(zapFlagSet)
	controllerCmd.Flags().AddGoFlagSet(zapFlagSet)

	controllerCmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	controllerCmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	controllerCmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	registerJsonnetLibraryNamespaceFlag(controllerCmd)
}

var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "Starts the controller manager",
	Long:  "Starts the controller manager",
	Run:   runController,
}

func runController(cmd *cobra.Command, _ []string) {
	setupLog := ctrl.Log.WithName("setup")
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(espejotev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	restConf := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(restConf, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "f7157d46.espejote.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	lifetimeCtx := cmd.Context()

	mrr := &controllers.ManagedResourceReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("managed-resource-controller"),

		ControllerLifetimeCtx:   lifetimeCtx,
		JsonnetLibraryNamespace: jsonnetLibraryNamespace,
	}
	if err := mrr.Setup(restConf, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ManagedResource")
		os.Exit(1)
	}
	metrics.Registry.MustRegister(&controllers.CacheSizeCollector{ManagedResourceReconciler: mrr})
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(lifetimeCtx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
