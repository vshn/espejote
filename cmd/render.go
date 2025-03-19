package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"

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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/yaml"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

var yellow = color.New(color.FgYellow, color.Bold).SprintFunc()

func init() {
	rootCmd.AddCommand(NewRenderCommand())
}

func NewRenderCommand() *cobra.Command {
	c := &cobra.Command{
		Use:       "render path",
		Short:     yellow("ALPHA") + " Renders a ManagedResource.",
		Long:      yellow("ALPHA") + " Renders the given ManagedResource and prints the result to stdout.",
		ValidArgs: []string{"path"},
		Args:      cobra.ExactArgs(1),
		Run:       runRender,
	}
	registerJsonnetLibraryNamespaceFlag(c)
	c.Flags().StringP("namespace", "n", "", "Namespace to use as the context for the ManagedResource. Overrides the given ManagedResource's namespace.")
	c.Flags().StringArrayP("input", "I", nil, "Input files to use for rendering. If not set the cluster will be used to load triggers, context, libraries.")
	return c
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
	Name      string                       `json:"name"`
	Resources []*unstructured.Unstructured `json:"resources"`
}

func runRender(cmd *cobra.Command, args []string) {
	renderNamespace, rnerr := cmd.Flags().GetString("namespace")
	renderInputs, rierr := cmd.Flags().GetStringArray("input")
	if err := multierr.Combine(rnerr, rierr); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get flags: %s\n", err.Error())
		os.Exit(1)
	}

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

	if len(renderInputs) > 0 {
		renderFromInputs(ctx, cmd.OutOrStdout(), mr, renderInputs)
		return
	}
	renderFromCluster(ctx, cmd.OutOrStdout(), mr, renderNamespace)
}

func renderFromInputs(ctx context.Context, out io.Writer, mr espejotev1alpha1.ManagedResource, renderInputs []string) {
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
		fmt.Fprintf(os.Stderr, "Failed to load some inputs: %s\n", err.Error())
		os.Exit(1)
	}

	for _, input := range inputs {
		contexts := make(map[string][]*unstructured.Unstructured)
		for _, context := range input.Context {
			if _, ok := contexts[context.Name]; ok {
				fmt.Fprintf(os.Stderr, "Duplicate context name: %s\n", context.Name)
				os.Exit(1)
			}
			contexts[context.Name] = context.Resources
		}

		imports := make(map[string]jsonnet.Contents)
		for name, code := range input.Libraries {
			imports[name] = jsonnet.MakeContents(code)
		}
		imports["espejote.libsonnet"] = jsonnet.MakeContents(controllers.EspejoteLibsonnet)

		for _, trigger := range input.Triggers {
			triggerClientGetter := func(triggerName string) (client.Reader, error) { return nil, fmt.Errorf("not implemented") }
			ti := controllers.TriggerInfo{TriggerName: trigger.Name}

			if trigger.WatchResource != nil {
				triggerClientGetter = func(triggerName string) (client.Reader, error) {
					return &staticUnstructuredReader{
						objs: []*unstructured.Unstructured{trigger.WatchResource},
					}, nil
				}
				ti.WatchResource = watchResourceFromObj(trigger.WatchResource)
			}

			renderer := &controllers.Renderer{
				Importer:            &jsonnet.MemoryImporter{Data: imports},
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
				fmt.Fprintf(os.Stderr, "Failed to render: %s\n", err.Error())
				os.Exit(1)
			}

			if err := printRendered(out, rendered, ti); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to print rendered output: %s\n", err.Error())
				os.Exit(1)
			}
		}
	}
}

type staticUnstructuredReader struct {
	objs []*unstructured.Unstructured
}

func (r *staticUnstructuredReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if len(r.objs) != 1 {
		return fmt.Errorf("Get called on staticReader with %d objects, expected exactly 1 object", len(r.objs))
	}

	uo, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("expected *unstructured.Unstructured, got %T", obj)
	}

	*uo = *r.objs[0]
	return nil
}

func (r *staticUnstructuredReader) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	l := make([]runtime.Object, len(r.objs))
	for i := range r.objs {
		l[i] = r.objs[i]
	}

	if err := meta.SetList(list, l); err != nil {
		return fmt.Errorf("failed to set list: %w", err)
	}
	return nil
}

func renderFromCluster(ctx context.Context, out io.Writer, mr espejotev1alpha1.ManagedResource, renderNamespace string) {
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(espejotev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme

	if renderNamespace != "" {
		mr.Namespace = renderNamespace
	}
	if mr.Namespace == "" {
		warn := yellow("WARNING:")
		fmt.Fprintf(os.Stderr, "%s No namespace in --namespace or given managed resource. Using 'default'. This impacts triggers, context, and library loading.\n", warn)
		mr.Namespace = "default"
	}

	restConf := ctrl.GetConfigOrDie()
	c, err := client.New(restConf, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create client: %s\n", err.Error())
		os.Exit(1)
	}

	triggerCfgs := map[string]espejotev1alpha1.ManagedResourceTrigger{}
	for _, trigger := range mr.Spec.Triggers {
		if _, ok := triggerCfgs[trigger.Name]; ok {
			fmt.Fprintf(os.Stderr, "Duplicate trigger name: %s\n", trigger.Name)
			os.Exit(1)
		}
		triggerCfgs[trigger.Name] = trigger
	}
	contextCfgs := map[string]espejotev1alpha1.ManagedResourceContext{}
	for _, context := range mr.Spec.Context {
		if _, ok := contextCfgs[context.Name]; ok {
			fmt.Fprintf(os.Stderr, "Duplicate context name: %s\n", context.Name)
			os.Exit(1)
		}
		contextCfgs[context.Name] = context
	}
	readerForTrigger := func(triggerName string) (client.Reader, error) {
		sel, err := objectSelectorForClusterResource(c, triggerCfgs[triggerName].WatchResource, mr.Namespace)
		if err != nil {
			return nil, err
		}
		return &objectSelectingReader{c, sel}, nil
	}

	triggers := []controllers.TriggerInfo{{}}
	tColErrs := []error{}
	for _, trigger := range mr.Spec.Triggers {
		if trigger.Interval.Duration != 0 {
			triggers = append(triggers, controllers.TriggerInfo{TriggerName: trigger.Name})
			continue
		}

		if trigger.WatchResource.APIVersion == "" {
			tColErrs = append(tColErrs, fmt.Errorf("trigger %q has neither interval or APIVersion", trigger.Name))
			continue
		}

		r, err := readerForTrigger(trigger.Name)
		if err != nil {
			tColErrs = append(tColErrs, fmt.Errorf("failed to create reader for trigger %q: %w", trigger.Name, err))
		}
		t := &unstructured.Unstructured{}
		t.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   trigger.WatchResource.GetGroup(),
			Version: trigger.WatchResource.GetAPIVersion(),
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
			triggers = append(triggers, controllers.TriggerInfo{
				TriggerName:   trigger.Name,
				WatchResource: watchResourceFromObj(&obj),
			})
		}
	}
	if err := multierr.Combine(tColErrs...); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load some triggers: %s\n", err.Error())
		os.Exit(1)
	}

	renderer := &controllers.Renderer{
		Importer:            controllers.FromClientImporter(c, mr.Namespace, jsonnetLibraryNamespace),
		TriggerClientGetter: readerForTrigger,
		ContextClientGetter: func(contextName string) (client.Reader, error) {
			sel, err := objectSelectorForClusterResource(c, contextCfgs[contextName].Resource, mr.Namespace)
			if err != nil {
				return nil, err
			}
			return &objectSelectingReader{c, sel}, nil
		},
	}

	for _, trigger := range triggers {
		rendered, err := renderer.Render(ctx, mr, trigger)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to render: %s\n", err.Error())
			os.Exit(1)
		}

		if err := printRendered(out, rendered, trigger); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to print rendered output: %s\n", err.Error())
			os.Exit(1)
		}
	}
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
		Version: cr.GetAPIVersion(),
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
