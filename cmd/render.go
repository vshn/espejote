package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/fatih/color"
	"github.com/google/go-jsonnet"
	"github.com/spf13/cobra"
	"go.uber.org/multierr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/yaml"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

var yellow = color.New(color.FgYellow, color.Bold).SprintFunc()
var bgBlue = color.New(color.BgBlue, color.Bold).SprintFunc()
var bold = color.New(color.Bold).SprintFunc()

func init() {
	rootCmd.AddCommand(NewRenderCommand(ctrl.GetConfig))
}

func NewRenderCommand(
	restConfigFunc func() (*rest.Config, error),
) *cobra.Command {
	cc := &renderCommandConfig{
		restConfigFunc: restConfigFunc,
	}
	c := &cobra.Command{
		Use:     "render path",
		Example: "espejote render path/to/managedresource.yaml --input path/to/input.yaml",
		Short:   yellow("ALPHA") + " Renders a ManagedResource.",
		Long: strings.Join([]string{
			yellow("ALPHA"), "Renders the given ManagedResource and prints the result to stdout.",
			"\n\nThe command supports two modes: rendering from the cluster and rendering from input files.",
			"\nInput files can be passed with the --input flag. If not set, the command will use the cluster from the current kubeconfig to load triggers, context, and libraries.",
			"\nInput files must be in YAML format and contain the following fields:",
			"\n  - triggers: A list of triggers to render for. Should always include an empty trigger to render a general reconciliation. The command will render the ManagedResource for each trigger defined in the input files.",
			"\n  - context: A list of contexts to render for.",
			"\n  - libraries: A map of used library names to their code.",
			"\n\n Inputs are currently", bold("not validated"), "and must be correct.",
			"Inputs can be generated from the cluster with the", bgBlue(" collect-input "), "command.",
		}, " "),
		ValidArgs: []string{"path"},
		Args:      cobra.ExactArgs(1),
		RunE:      cc.runRender,
	}
	registerJsonnetLibraryNamespaceFlag(c)
	c.Flags().StringP("namespace", "n", "", "Namespace to use as the context for the ManagedResource. Overrides the given ManagedResource's namespace.")
	c.Flags().StringArrayP("input", "I", nil, "Input files to use for rendering. If not set the cluster will be used to load triggers, context, libraries.")
	return c
}

type renderCommandConfig struct {
	restConfigFunc func() (*rest.Config, error)
}

type RenderInput struct {
	Triggers  []RenderInputTrigger `json:"triggers"`
	Context   []RenderInputContext `json:"context"`
	Libraries map[string]string    `json:"libraries"`
}

type RenderInputTrigger struct {
	Name          string                     `json:"name"`
	WatchResource *unstructured.Unstructured `json:"watchResource"`
}

type RenderInputContext struct {
	Name      string                      `json:"name"`
	Resources []unstructured.Unstructured `json:"resources"`
}

func (rcc *renderCommandConfig) runRender(cmd *cobra.Command, args []string) error {
	renderNamespace, rnerr := cmd.Flags().GetString("namespace")
	renderInputs, rierr := cmd.Flags().GetStringArray("input")
	jsonnetLibraryNamespace, jlnerr := cmd.Flags().GetString("jsonnet-library-namespace")
	if err := multierr.Combine(rnerr, rierr, jlnerr); err != nil {
		return fmt.Errorf("failed to get flags: %w", err)
	}

	ctx := cmd.Context()

	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read managed resource file: %w", err)
	}

	var mr espejotev1alpha1.ManagedResource
	if err := yaml.UnmarshalStrict(raw, &mr); err != nil {
		return fmt.Errorf("failed to unmarshal managed resource file: %w", err)
	}

	if len(renderInputs) > 0 {
		return rcc.renderFromInputFiles(ctx, cmd.OutOrStdout(), mr, renderInputs)
	}
	return rcc.renderFromCluster(ctx, cmd.OutOrStdout(), mr, renderNamespace, jsonnetLibraryNamespace)
}

func (rcc *renderCommandConfig) renderFromInputFiles(ctx context.Context, out io.Writer, mr espejotev1alpha1.ManagedResource, renderInputs []string) error {
	inputs := make([]RenderInput, len(renderInputs))
	inputErrs := make([]error, len(renderInputs))
	for _, input := range renderInputs {
		raw, err := os.ReadFile(input)
		if err != nil {
			inputErrs = append(inputErrs, fmt.Errorf("failed to read input file %q: %w", input, err))
			continue
		}
		var ri RenderInput
		if err := yaml.UnmarshalStrict(raw, &ri); err != nil {
			inputErrs = append(inputErrs, fmt.Errorf("failed to unmarshal input file %q: %w", input, err))
			continue
		}
		inputs = append(inputs, ri)
	}
	if err := multierr.Combine(inputErrs...); err != nil {
		return fmt.Errorf("some inputs failed to load: %w", err)
	}

	for _, input := range inputs {
		if err := renderFromInput(ctx, out, mr, input, nil); err != nil {
			return fmt.Errorf("failed to render from input: %w", err)
		}
	}

	return nil
}

// renderFromInput renders the given ManagedResource using the given input.
// The importer is used to load libraries.
// If the importer is nil, the RenderInput.Libraries are used as libraries.
func renderFromInput(ctx context.Context, out io.Writer, mr espejotev1alpha1.ManagedResource, input RenderInput, importer jsonnet.Importer) error {
	contexts := make(map[string][]unstructured.Unstructured)
	for _, context := range input.Context {
		if _, ok := contexts[context.Name]; ok {
			return fmt.Errorf("duplicate context name: %s", context.Name)
		}
		contexts[context.Name] = context.Resources
	}

	if importer == nil {
		imports := make(map[string]jsonnet.Contents)
		for name, code := range input.Libraries {
			imports[name] = jsonnet.MakeContents(code)
		}
		imports["espejote.libsonnet"] = jsonnet.MakeContents(controllers.EspejoteLibsonnet)
		importer = &jsonnet.MemoryImporter{Data: imports}
	}

	for _, trigger := range input.Triggers {
		triggerClientGetter := func(triggerName string) (client.Reader, error) { return nil, fmt.Errorf("not implemented") }
		ti := controllers.TriggerInfo{TriggerName: trigger.Name}

		if trigger.WatchResource != nil {
			triggerClientGetter = func(triggerName string) (client.Reader, error) {
				return &staticUnstructuredReader{
					objs: []unstructured.Unstructured{*trigger.WatchResource},
				}, nil
			}
			ti.WatchResource = watchResourceFromObj(trigger.WatchResource)
		}

		renderer := &controllers.Renderer{
			Importer:            importer,
			TriggerClientGetter: triggerClientGetter,
			ContextClientGetter: func(contextName string) (client.Reader, error) {
				resources, ok := contexts[contextName]
				if !ok {
					return nil, fmt.Errorf("context %q not found", contextName)
				}
				return &staticUnstructuredReader{
					objs: resources,
				}, nil
			},
		}

		rendered, err := renderer.Render(ctx, mr, ti)
		if err != nil {
			return fmt.Errorf("failed to render: %w", err)
		}

		if err := printRendered(out, rendered, ti); err != nil {
			return fmt.Errorf("failed to print rendered output: %w", err)
		}
	}

	return nil
}

type staticUnstructuredReader struct {
	objs []unstructured.Unstructured
}

func (r *staticUnstructuredReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if len(r.objs) != 1 {
		return fmt.Errorf("Get called on staticReader with %d objects, expected exactly 1 object", len(r.objs))
	}

	uo, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("expected *unstructured.Unstructured, got %T", obj)
	}

	*uo = r.objs[0]
	return nil
}

func (r *staticUnstructuredReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	l := make([]runtime.Object, len(r.objs))
	for i := range r.objs {
		l[i] = &r.objs[i]
	}

	if err := meta.SetList(list, l); err != nil {
		return fmt.Errorf("failed to set list: %w", err)
	}
	return nil
}

func (rcc *renderCommandConfig) renderFromCluster(ctx context.Context, out io.Writer, mr espejotev1alpha1.ManagedResource, renderNamespace, jsonnetLibraryNamespace string) error {
	scheme := newScheme()

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

	if err := renderFromInput(ctx, out, mr, input, importer); err != nil {
		return fmt.Errorf("failed to render input: %w", err)
	}

	return nil
}

func printRendered(w io.Writer, rendered string, trigger controllers.TriggerInfo) error {
	triggerJson, terr := json.Marshal(trigger)
	renderedYaml, yerr := yaml.JSONToYAML([]byte(rendered))
	if err := multierr.Combine(terr, yerr); err != nil {
		return fmt.Errorf("failed to render for output: %w", err)
	}

	fmt.Fprintln(w, "---")
	if trigger.TriggerName != "" {
		fmt.Fprintln(w, "# Trigger:", string(triggerJson))
	}
	fmt.Fprint(w, string(renderedYaml))

	return nil
}

type objectSelectingReader struct {
	c   client.Client
	sel *objectSelector
}

func (r *objectSelectingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if r.sel.target.GroupVersionKind() != obj.GetObjectKind().GroupVersionKind() {
		return fmt.Errorf("object key %q does not match watch target %q", key, r.sel.target.GroupVersionKind())
	}

	if !r.sel.fieldsSelector.Matches(fields.Set{
		"metadata.name":      key.Name,
		"metadata.namespace": key.Namespace,
	}) {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	if err := r.c.Get(ctx, key, obj, opts...); err != nil {
		return err
	}

	if !r.sel.labelSelector.Matches(labels.Set(obj.GetLabels())) {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	if !r.sel.filterFunc(obj) {
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	}

	return nil
}

func (r *objectSelectingReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if r.sel.target.GroupVersionKind() != list.GetObjectKind().GroupVersionKind() {
		return fmt.Errorf("object list does not match watch target %q", r.sel.target.GroupVersionKind())
	}

	lo := new(client.ListOptions)
	for _, opt := range opts {
		opt.ApplyToList(lo)
	}
	if lo.FieldSelector != nil || lo.LabelSelector != nil {
		return fmt.Errorf("field and label selectors are not supported for list operations")
	}

	opts = append(opts, client.MatchingFieldsSelector{Selector: r.sel.fieldsSelector}, client.MatchingLabelsSelector{Selector: r.sel.labelSelector})
	if err := r.c.List(ctx, list, opts...); err != nil {
		return err
	}

	l, err := meta.ExtractList(list)
	if err != nil {
		return fmt.Errorf("failed to extract list for filtering: %w", err)
	}

	var filtered []runtime.Object
	for _, rawObj := range l {
		obj, ok := rawObj.(client.Object)
		if !ok {
			return fmt.Errorf("expected client.Object in list")
		}
		if r.sel.filterFunc(obj) {
			filtered = append(filtered, obj)
		}
	}

	if err := meta.SetList(list, filtered); err != nil {
		return fmt.Errorf("failed to set filtered list: %w", err)
	}

	return nil
}

type objectSelector struct {
	target         unstructured.Unstructured
	fieldsSelector fields.Selector
	labelSelector  labels.Selector
	filterFunc     func(client.Object) bool
}

func objectSelectorForClusterResource(c client.Client, cr espejotev1alpha1.ClusterResource, defaultNamespace string) (*objectSelector, error) {
	watchTarget := &unstructured.Unstructured{}
	watchTarget.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   cr.GetGroup(),
		Version: cr.GetVersion(),
		Kind:    cr.GetKind(),
	})

	isNamespaced, err := apiutil.IsObjectNamespaced(watchTarget, c.Scheme(), c.RESTMapper())
	if err != nil {
		return nil, fmt.Errorf("failed to determine if watch target %q is namespaced: %w", cr, err)
	}

	var sel []fields.Selector
	if name := cr.GetName(); name != "" {
		sel = append(sel, fields.OneTermEqualSelector("metadata.name", name))
	}
	if ns := ptr.Deref(cr.GetNamespace(), defaultNamespace); isNamespaced && ns != "" {
		sel = append(sel, fields.OneTermEqualSelector("metadata.namespace", ns))
	}

	lblSel := labels.Everything()
	if cr.GetLabelSelector() != nil {
		s, err := metav1.LabelSelectorAsSelector(cr.GetLabelSelector())
		if err != nil {
			return nil, fmt.Errorf("failed to parse label selector for trigger %q: %w", cr, err)
		}
		lblSel = s
	}

	filter := func(o client.Object) bool { return true }
	if len(cr.GetMatchNames()) > 0 || len(cr.GetIgnoreNames()) > 0 {
		filter = func(o client.Object) bool {
			return (len(cr.GetMatchNames()) == 0 || slices.Contains(cr.GetMatchNames(), o.GetName())) &&
				!slices.Contains(cr.GetIgnoreNames(), o.GetName())
		}
	}

	return &objectSelector{
		target:         *watchTarget,
		fieldsSelector: fields.AndSelectors(sel...),
		labelSelector:  lblSel,
		filterFunc:     filter,
	}, nil
}

func watchResourceFromObj(obj client.Object) controllers.WatchResource {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return controllers.WatchResource{
		Group:      gvk.Group,
		APIVersion: gvk.Version,
		Kind:       gvk.Kind,
		Name:       obj.GetName(),
		Namespace:  obj.GetNamespace(),
	}
}
