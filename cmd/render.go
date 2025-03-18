package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

func init() {
	rootCmd.AddCommand(renderCmd)
	registerJsonnetLibraryNamespaceFlag(renderCmd)
}

var renderCmd = &cobra.Command{
	Use:       "render path",
	Short:     "Renders a ManagedResource",
	Long:      "Renders the given ManagedResource and prints the result to stdout.",
	ValidArgs: []string{"path"},
	Args:      cobra.ExactArgs(1),
	Run:       runRender,
}

func runRender(cmd *cobra.Command, args []string) {
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(espejotev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	ctx := cmd.Context()

	raw, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read file: %s\n", err.Error())
		os.Exit(1)
	}

	var mr espejotev1alpha1.ManagedResource
	if err := yaml.UnmarshalStrict(raw, &mr); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to unmarshal file: %s\n", err.Error())
		os.Exit(1)
	}

	restConf := ctrl.GetConfigOrDie()
	c, err := client.New(restConf, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %s\n", err.Error())
		os.Exit(1)
	}

	rendered, err := (&controllers.Renderer{
		Importer: controllers.FromClientImporter(c, "default", jsonnetLibraryNamespace),
		TriggerClientGetter: func(triggerName string) (client.Reader, error) {
			return c, nil
		},
		ContextClientGetter: func(contextName string) (client.Reader, error) {
			return c, nil
		},
	}).Render(ctx, mr, controllers.TriggerInfo{})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to render: %s\n", err.Error())
		os.Exit(1)
	}

	fmt.Println("---")
	fmt.Println(rendered)
}
