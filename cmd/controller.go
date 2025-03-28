package cmd

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/multierr"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/vshn/espejote/admission"
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

	defaultNamespace := "default"
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		defaultNamespace = ns
	}
	controllerCmd.Flags().String("controller-namespace", defaultNamespace, "The namespace the controller runs in.")

	controllerCmd.Flags().Bool("enable-dynamic-admission-webhook", true, "Enable the dynamic admission webhook.")
	controllerCmd.Flags().String("dynamic-admission-webhook-service-name", "espejote-webhook-service", "The name of the service that serves the dynamic admission webhook.")
	controllerCmd.Flags().String("dynamic-admission-webhook-name", "espejote-dynamic-webhook", "The name of the dynamic admission webhook.")
	controllerCmd.Flags().Int32("dynamic-admission-webhook-port", 9443, "The port the dynamic admission webhook listens on.")

	controllerCmd.Flags().String("webhook-cert-path", "", "The directory that contains the webhook certificate.")
	controllerCmd.Flags().String("webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	controllerCmd.Flags().String("webhook-cert-key", "tls.key", "The name of the webhook key file.")

	registerJsonnetLibraryNamespaceFlag(controllerCmd)
}

var controllerCmd = &cobra.Command{
	Use:   "controller",
	Short: "Starts the controller manager",
	Long:  "Starts the controller manager",
	RunE:  runController,
}

func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(espejotev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
	return scheme
}

func runController(cmd *cobra.Command, _ []string) error {
	jsonnetLibraryNamespace, jlnerr := cmd.Flags().GetString("jsonnet-library-namespace")
	controllerNamespace, cnerr := cmd.Flags().GetString("controller-namespace")
	enableDynamicAdmissionWebhook, edawerr := cmd.Flags().GetBool("enable-dynamic-admission-webhook")
	dynamicAdmissionWebhookServiceName, dawsnerr := cmd.Flags().GetString("dynamic-admission-webhook-service-name")
	dynamicAdmissionWebhookName, dawnerr := cmd.Flags().GetString("dynamic-admission-webhook-name")
	dynamicAdmissionWebhookPort, dawperr := cmd.Flags().GetInt32("dynamic-admission-webhook-port")
	webhookCertPath, wcperr := cmd.Flags().GetString("webhook-cert-path")
	webhookCertName, wcnerr := cmd.Flags().GetString("webhook-cert-name")
	webhookCertKey, wckerr := cmd.Flags().GetString("webhook-cert-key")
	if err := multierr.Combine(jlnerr, cnerr, dawsnerr, wcperr, wcnerr, wckerr, edawerr, dawnerr, dawperr); err != nil {
		return fmt.Errorf("failed to get flags: %w", err)
	}

	cmd.Println("Starting the controller manager",
		"jsonnet-library-namespace", jsonnetLibraryNamespace,
		"controller-namespace", controllerNamespace,
		"enable-dynamic-admission-webhook", enableDynamicAdmissionWebhook,
		"dynamic-admission-webhook-service-name", dynamicAdmissionWebhookServiceName,
		"dynamic-admission-webhook-name", dynamicAdmissionWebhookName,
	)

	scheme := newScheme()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	var webhookCertWatcher *certwatcher.CertWatcher

	var webhookTLSOpts []func(*tls.Config)
	if len(webhookCertPath) > 0 {
		cmd.Println("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			return fmt.Errorf("failed to initialize webhook certificate watcher: %w", err)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	restConf := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(restConf, ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		WebhookServer:          webhookServer,
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
		return fmt.Errorf("unable to start manager: %w", err)
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
		return fmt.Errorf("unable to create ManagedResource controller: %w", err)
	}
	metrics.Registry.MustRegister(&controllers.CacheSizeCollector{ManagedResourceReconciler: mrr})

	if enableDynamicAdmissionWebhook {
		if err := (&controllers.AdmissionReconciler{
			Client: mgr.GetClient(),

			MutatingWebhookName:   dynamicAdmissionWebhookName,
			ValidatingWebhookName: dynamicAdmissionWebhookName,

			WebhookPort:         dynamicAdmissionWebhookPort,
			WebhookServiceName:  dynamicAdmissionWebhookServiceName,
			ControllerNamespace: controllerNamespace,
		}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("unable to create ManagedResourceReconciler controller: %w", err)
		}

		mgr.GetWebhookServer().Register("/dynamic/{namespace}/{name}", admission.NewHandler(mgr.GetClient(), jsonnetLibraryNamespace))
	}

	//+kubebuilder:scaffold:builder

	if webhookCertWatcher != nil {
		cmd.Println("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			return fmt.Errorf("unable to add webhook certificate watcher to manager: %w", err)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	cmd.Println("Starting the controller manager")
	if err := mgr.Start(lifetimeCtx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}
	return nil
}
