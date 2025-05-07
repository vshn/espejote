package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

func init() {
	RootCmd.AddCommand(NewCollectInputCommand(ctrl.GetConfig))
}

func NewCollectInputCommand(
	restConfigFunc func() (*rest.Config, error),
) *cobra.Command {
	cc := &collectInputCommandConfig{
		restConfigFunc: restConfigFunc,
	}
	c := &cobra.Command{
		Use:       "collect-input path",
		Example:   "espejote collect-input path/to/managedresource.yaml > input.yaml",
		Short:     yellow("ALPHA") + " Creates a input file for the " + bgBlue(" render ") + " command.",
		Long:      yellow("ALPHA") + " Creates a input file for the " + bgBlue(" render ") + " command.\n\nConnects to the the Kubernetes cluster in the current kubeconfig to collect the necessary data.",
		ValidArgs: []string{"path"},
		Args:      cobra.ExactArgs(1),
		RunE:      cc.runCollectInput,
	}
	registerJsonnetLibraryNamespaceFlag(c)
	c.Flags().StringP("namespace", "n", "", "Namespace to use as the context for the ManagedResource. Overrides the given ManagedResource's namespace.")
	return c
}

type collectInputCommandConfig struct {
	restConfigFunc func() (*rest.Config, error)
}

func (rcc *collectInputCommandConfig) runCollectInput(cmd *cobra.Command, args []string) error {
	renderNamespace, nserr := cmd.Flags().GetString("namespace")
	jsonnetLibraryNamespace, jnserr := cmd.Flags().GetString("jsonnet-library-namespace")
	if err := multierr.Combine(nserr, jnserr); err != nil {
		return fmt.Errorf("failed to get flags: %w", err)
	}

	ctx := cmd.Context()
	scheme := newScheme()

	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read managed resource file: %w", err)
	}

	var mr espejotev1alpha1.ManagedResource
	if err := yaml.UnmarshalStrict(raw, &mr); err != nil {
		return fmt.Errorf("failed to unmarshal managed resource file: %w", err)
	}

	if renderNamespace != "" {
		mr.Namespace = renderNamespace
	}
	if mr.Namespace == "" {
		warn := yellow("WARNING:")
		fmt.Fprintf(os.Stderr, "%s No namespace in --namespace or given managed resource. Using 'default'. This impacts triggers, context, and library loading.\n", warn)
		mr.Namespace = "default"
	}

	restConf, err := rcc.restConfigFunc()
	if err != nil {
		return fmt.Errorf("failed to get rest config: %w", err)
	}
	c, err := client.New(restConf, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	input, err := collectInput(ctx, c, mr, renderNamespace)
	if err != nil {
		return fmt.Errorf("failed to collect input: %w", err)
	}

	importer := controllers.FromClientImporter(c, mr.Namespace, jsonnetLibraryNamespace)

	// Render the input to know which libraries are imported
	if err := renderFromInput(ctx, io.Discard, mr, input, importer); err != nil {
		return fmt.Errorf("failed to render input: %w", err)
	}

	input.Libraries = map[string]string{}
	for _, v := range importer.Cache {
		input.Libraries[v.FoundAt] = v.Contents.String()
	}

	yaml, err := yaml.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to encode collected input: %w", err)
	}
	if _, err := cmd.OutOrStdout().Write(yaml); err != nil {
		return fmt.Errorf("failed to write collected input: %w", err)
	}

	return nil
}

func collectInput(ctx context.Context, c client.Client, mr espejotev1alpha1.ManagedResource, renderNamespace string) (RenderInput, error) {
	contextCfgs := map[string]espejotev1alpha1.ManagedResourceContext{}
	for _, context := range mr.Spec.Context {
		if _, ok := contextCfgs[context.Name]; ok {
			return RenderInput{}, fmt.Errorf("duplicate context name: %s", context.Name)
		}
		contextCfgs[context.Name] = context
	}
	triggerCfgs := map[string]espejotev1alpha1.ManagedResourceTrigger{}
	for _, trigger := range mr.Spec.Triggers {
		if _, ok := triggerCfgs[trigger.Name]; ok {
			return RenderInput{}, fmt.Errorf("duplicate trigger name: %s", trigger.Name)
		}
		if trigger.WatchContextResource.Name != "" {
			cfg, ok := contextCfgs[trigger.WatchContextResource.Name]
			if !ok {
				return RenderInput{}, fmt.Errorf("trigger %q references unknown context %q", trigger.Name, trigger.WatchContextResource.Name)
			}
			triggerCfgs[trigger.Name] = espejotev1alpha1.ManagedResourceTrigger{
				Name:          trigger.Name,
				WatchResource: espejotev1alpha1.TriggerWatchResource(cfg.Resource),
			}
			continue
		}
		triggerCfgs[trigger.Name] = trigger
	}
	readerForTrigger := func(triggerName string) (client.Reader, error) {
		sel, err := objectSelectorForClusterResource(c, triggerCfgs[triggerName].WatchResource, mr.Namespace)
		if err != nil {
			return nil, err
		}
		return &objectSelectingReader{c, sel}, nil
	}

	triggers := []RenderInputTrigger{{}}
	tColErrs := []error{}
	for _, trigger := range mr.Spec.Triggers {
		trigger = triggerCfgs[trigger.Name]

		if trigger.Interval.Duration != 0 {
			triggers = append(triggers, RenderInputTrigger{Name: trigger.Name})
			continue
		}

		if trigger.WatchResource.APIVersion == "" {
			tColErrs = append(tColErrs, fmt.Errorf("trigger %q has neither interval or APIVersion", trigger.Name))
			continue
		}

		r, err := readerForTrigger(trigger.Name)
		if err != nil {
			tColErrs = append(tColErrs, fmt.Errorf("failed to create reader for trigger %q: %w", trigger.Name, err))
			continue
		}
		t := &unstructured.Unstructured{}
		t.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   trigger.WatchResource.GetGroup(),
			Version: trigger.WatchResource.GetVersion(),
			Kind:    trigger.WatchResource.GetKind(),
		})
		tl, err := t.ToList()
		if err != nil {
			tColErrs = append(tColErrs, fmt.Errorf("failed to create list for trigger %q: %w", trigger.Name, err))
			continue
		}
		if err := r.List(ctx, tl); err != nil {
			tColErrs = append(tColErrs, fmt.Errorf("failed to list objects for trigger %q: %w", trigger.Name, err))
			continue
		}

		triggers = slices.Grow(triggers, len(tl.Items))
		for _, obj := range tl.Items {
			triggers = append(triggers, RenderInputTrigger{
				Name:          trigger.Name,
				WatchResource: &obj,
			})
		}
	}
	if err := multierr.Combine(tColErrs...); err != nil {
		return RenderInput{}, fmt.Errorf("some triggers failed to collect: %w", err)
	}

	contexts := make([]RenderInputContext, 0, len(mr.Spec.Context))
	contextColErrs := []error{}
	for _, context := range mr.Spec.Context {
		sel, err := objectSelectorForClusterResource(c, contextCfgs[context.Name].Resource, mr.Namespace)
		if err != nil {
			contextColErrs = append(contextColErrs, fmt.Errorf("failed to create object selector for context %q: %w", context.Name, err))
			continue
		}

		t := &unstructured.Unstructured{}
		t.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   context.Resource.GetGroup(),
			Version: context.Resource.GetVersion(),
			Kind:    context.Resource.GetKind(),
		})
		tl, err := t.ToList()
		if err != nil {
			tColErrs = append(tColErrs, fmt.Errorf("failed to create list for context %q: %w", context.Name, err))
			continue
		}
		if err := (&objectSelectingReader{c, sel}).List(ctx, tl); err != nil {
			contextColErrs = append(contextColErrs, fmt.Errorf("failed to list objects for context %q: %w", context.Name, err))
			continue
		}

		contexts = append(contexts, RenderInputContext{
			Name:      context.Name,
			Resources: tl.Items,
		})
	}
	if err := multierr.Combine(contextColErrs...); err != nil {
		return RenderInput{}, fmt.Errorf("some contexts failed to collect: %w", err)
	}

	return RenderInput{
		Triggers: triggers,
		Context:  contexts,
	}, nil
}
