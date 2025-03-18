package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
)

var jsonnetLibraryNamespace string

func registerJsonnetLibraryNamespaceFlag(cmd *cobra.Command) {
	defaultNamespace := "default"
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		defaultNamespace = ns
	}

	cmd.Flags().StringVar(&jsonnetLibraryNamespace, "jsonnet-library-namespace", defaultNamespace, "The namespace to look for Jsonnet libraries in.")
}

var rootCmd = &cobra.Command{
	Use:   "espejote",
	Short: "Espejote manages arbitrary resources in a Kubernetes cluster.",
	Long:  `Espejote manages resources by server-side applying rendered Jsonnet manifests to the cluster. It allows fine-grained control over external context used to rendering the resources and the triggers that cause the resources to be applied.`,
}

func Execute() {
	lifetimeCtx := ctrl.SetupSignalHandler()

	if err := rootCmd.ExecuteContext(lifetimeCtx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
