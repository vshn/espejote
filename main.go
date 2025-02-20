package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/vshn/espejote/cmd"
)

const (
	textMetricsAddr = `The address the metrics endpoint binds to.
Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.`
	textProbeAddr      = `The address the probe endpoint binds to.`
	textEnableElection = `Enable leader election for controller manager.
Enabling this will ensure there is only one active controller manager.`
	textSecureMetrics = `If set, the metrics endpoint is served securely via HTTPS.
Use --metrics-secure=false to use HTTP instead.`
	textEnableHTTP2 = `If set, HTTP/2 will be enabled for the metrics and webhook servers`
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "espejote",
	Short: "Manages PrometheuRules on a cluster",
	Long: `This application watches for PrometheusRule resources.
This resources will be filtered and patched according
to the provided configuration. From the filtered and
patched resource a new PrometheuRule will be created.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("nothing to do here")
	},
}

// StartCmd will execute the controller-manager.
var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the controller-manager.",
	Run:   cmd.Start,
}

// ValidateCmd will validate the ManagedResource against the cluster.
var ValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the given ManagedResource against a cluster",
	Long: `Validate the given ManagedResource against a cluster.
No changes will be made on the cluster, but the
changes will be printed out.`,
	// Run: cmd.Validate,
}

// ValidateContextCmd will validate the context against the cluster.
// 👇 Update text and such
var ValidateContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Validate the given Context against a cluster",
	Long: `Validate the given Context against a cluster.
This will print out the parsed context.`,
	Args: cobra.ExactArgs(1),
	Run:  cmd.ValidateContext,
}

// ValidateSelectorCmd will validate the namespaceSelector against the cluster.
// 👇 Update text and such
var ValidateSelectorCmd = &cobra.Command{
	Use:   "selector",
	Short: "Validate the given NamespaceSelector against a cluster",
	Long: `Validate the given NamespaceSelector against a cluster.
This will print out the selected namespaces.`,
	Args: cobra.ExactArgs(1),
	Run:  cmd.ValidateNamespace,
}

// ValidateTemplateCmd will validate the template against the cluster.
var ValidateTemplateCmd = &cobra.Command{
	Use:   "template",
	Short: "Validate the given Template against a cluster",
	Long: `Validate the given Template against a cluster.
No changes will be made on the cluster, but the
changes will be printed out.`,
	Args: cobra.ExactArgs(1),
	Run:  cmd.ValidateTemplate,
}

func init() {
	cobra.OnInitialize(initConfig)

	StartCmd.PersistentFlags().String("metrics-address", "0", textMetricsAddr)
	StartCmd.PersistentFlags().String("probe-address", ":8081", textProbeAddr)
	StartCmd.PersistentFlags().Bool("metrics-secure", true, textSecureMetrics)
	StartCmd.PersistentFlags().Bool("enable-election", false, textEnableElection)
	StartCmd.PersistentFlags().Bool("enable-http2", false, textEnableHTTP2)
	ValidateCmd.Flags().String("kubeconfig", "$HOME/.kube/config", "Path to the kubeconfig file.")

	for _, err := range []error{
		viper.BindPFlag("metrics-address", StartCmd.PersistentFlags().Lookup("metrics-address")),
		viper.BindPFlag("probe-address", StartCmd.PersistentFlags().Lookup("probe-address")),
		viper.BindPFlag("metrics-secure", StartCmd.PersistentFlags().Lookup("metrics-secure")),
		viper.BindPFlag("enable-election", StartCmd.PersistentFlags().Lookup("enable-election")),
		viper.BindPFlag("enable-http2", StartCmd.PersistentFlags().Lookup("enable-http2")),
		viper.BindPFlag("kubeconfig", ValidateCmd.Flags().Lookup("kubeconfig")),
	} {
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	}

	RootCmd.AddCommand(StartCmd)
	RootCmd.AddCommand(ValidateCmd)
	ValidateCmd.AddCommand(ValidateContextCmd)
	ValidateCmd.AddCommand(ValidateSelectorCmd)
	ValidateCmd.AddCommand(ValidateTemplateCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv() // read in environment variables that match
}

func main() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
