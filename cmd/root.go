package cmd

import (
	"os"
	"sync"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

func registerJsonnetLibraryNamespaceFlag(cmd *cobra.Command) {
	defaultNamespace := "default"
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		defaultNamespace = ns
	}

	cmd.Flags().String("jsonnet-library-namespace", defaultNamespace, "The namespace to look for shared (`lib/`) Jsonnet libraries in.")
}

var RootCmd = &cobra.Command{
	Use:   "espejote",
	Short: "Espejote manages arbitrary resources in a Kubernetes cluster.",
	Long:  `Espejote manages resources by server-side applying rendered Jsonnet manifests to the cluster. It allows fine-grained control over external context used to rendering the resources and the triggers that cause the resources to be applied.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmd.SilenceUsage = true
	},
}

func Execute() {
	lifetimeCtx := ctrl.SetupSignalHandler()

	RootCmd.ExecuteContext(lifetimeCtx)
}

var forceColorCheck = sync.OnceFunc(func() {
	if os.Getenv("FORCE_COLOR") != "" {
		color.NoColor = false
	}
})

func yellow(s string) string {
	forceColorCheck()
	return color.New(color.FgYellow, color.Bold).Sprint(s)
}

func bgBlue(s string) string {
	forceColorCheck()
	return color.New(color.BgBlue, color.Bold).Sprint(s)
}

func bold(s string) string {
	forceColorCheck()
	return color.New(color.Bold).Sprint(s)
}
