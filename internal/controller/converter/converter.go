package converter

import (
	"context"
	"regexp"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/internal/dynamic"
	"github.com/vshn/espejote/internal/parser"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Converter interface {
	// GetParserName return the name used for this parser.
	GetParserName() string

	// GetContext returns context from API type.
	GetContext() []espejov1alpha1.Context

	// GetTemplate returns parser template from API type.
	GetTemplate() string

	// GetNamespaceSelector returns namespace selector from API type.
	GetNamespaceSelector() espejov1alpha1.NamespaceSelector

	// IsNamespaced returns true if its a namespaced API type.
	IsNamespaced() bool
}

func ToParser(ctx context.Context, clt *dynamic.Client, c Converter) (*parser.Parser, error) {
	l := log.FromContext(ctx)

	// Get Namespaces from reconciled resource.
	namespaces, err := convertToNamespaces(ctx, clt, c)
	if err != nil {
		l.Error(err, "convertToNamespaces")
		return nil, err
	}

	// Get Input from reconciled resource.
	// 👇 Maybe needs something done with passing dynamic client or smth
	input, err := convertToInput(ctx, clt, c, namespaces)
	if err != nil {
		l.Error(err, "convertToInput")
		return nil, err
	}

	// Return parser.
	return &parser.Parser{
		Name:       c.GetParserName(),
		Input:      input,
		Template:   c.GetTemplate(),
		Namespaces: convertToNamespaceList(namespaces),
	}, nil
}

func convertToInput(ctx context.Context, clt *dynamic.Client, c Converter, ns []string) (map[string]*unstructured.UnstructuredList, error) {
	input := make(map[string]*unstructured.UnstructuredList, 0)
	for _, item := range c.GetContext() {
		namespaceList := ns
		if item.Namespace != "" {
			// If Context item has Namespace defined use that.
			namespaceList = []string{item.Namespace}
		}

		list := &unstructured.UnstructuredList{}
		for _, ns := range namespaceList {
			dr, err := clt.Resource(item.APIVersion, item.Kind, ns)
			if err != nil {
				return nil, err
			}

			if item.Name != "" {
				ls, err := dr.Get(ctx, item.Name, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				list.Items = append(list.Items, *ls)
			} else {
				ls, err := dr.List(ctx, metav1.ListOptions{})
				if err != nil {
					return nil, err
				}
				list.Items = append(list.Items, ls.Items...)
			}
		}
		input[item.Alias] = list
	}

	return input, nil
}

func convertToNamespaces(ctx context.Context, clt *dynamic.Client, c Converter) ([]string, error) {
	if c.IsNamespaced() {
		return c.GetNamespaceSelector().MatchNames, nil
	}

	all, err := getAllNamespaces(ctx, clt)
	if err != nil {
		return nil, err
	}
	filtered := markedFromList(all)

	if c.GetNamespaceSelector().LabelSelector != nil {
		labeled, err := getLabeledNamespaces(ctx, clt, c.GetNamespaceSelector().LabelSelector)
		if err != nil {
			return nil, err
		}
		filtered = markMatchedNamespaces(labeled, filtered)
	}
	if c.GetNamespaceSelector().MatchNames != nil {
		filtered = markMatchedNamespaces(c.GetNamespaceSelector().MatchNames, filtered)
	}
	if c.GetNamespaceSelector().IgnoreNames != nil {
		filtered = unmarkMatchedNamespaces(c.GetNamespaceSelector().IgnoreNames, filtered)
	}

	return listFromMarked(filtered), nil
}

func convertToNamespaceList(namespaces []string) *corev1.NamespaceList {
	list := &corev1.NamespaceList{}
	for _, ns := range namespaces {
		list.Items = append(list.Items, corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		})
	}

	return list
}

func getAllNamespaces(ctx context.Context, clt *dynamic.Client) ([]string, error) {
	dr, err := clt.Resource("v1", "Namespace", "")
	if err != nil {
		return nil, err
	}

	list, err := dr.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	all := []string{}
	for _, ns := range list.Items {
		all = append(all, ns.GetName())
	}

	return all, nil
}

func getLabeledNamespaces(ctx context.Context, clt *dynamic.Client, ls *metav1.LabelSelector) ([]string, error) {
	dr, err := clt.Resource("v1", "Namespace", "")
	if err != nil {
		return nil, err
	}

	list, err := dr.List(ctx, metav1.ListOptions{
		LabelSelector: ls.String(),
	})
	if err != nil {
		return nil, err
	}

	all := []string{}
	for _, ns := range list.Items {
		all = append(all, ns.GetName())
	}

	return all, nil
}

func markMatchedNamespaces(matches []string, in map[string]bool) map[string]bool {
	for name := range in {
		for _, expr := range matches {
			rx := regexp.MustCompile(expr)
			if rx.MatchString(name) {
				// Add to filtered list but mark as false
				in[name] = true
			}
		}
	}

	return in
}

func unmarkMatchedNamespaces(matches []string, in map[string]bool) map[string]bool {
	for name := range in {
		for _, expr := range matches {
			rx := regexp.MustCompile(expr)
			if rx.MatchString(name) {
				// Add to filtered list but mark as false
				in[name] = false
			}
		}
	}

	return in
}

func markedFromList(list []string) map[string]bool {
	marked := make(map[string]bool, 0)
	for _, name := range list {
		marked[name] = false
	}

	return marked
}

func listFromMarked(marked map[string]bool) []string {
	list := []string{}
	for name, marked := range marked {
		if marked {
			list = append(list, name)
		}
	}

	return list
}

// -----------------------------------------------------------------------------

/*

// gvrFromContext returns the GroupVersionResource from the context item.
func gvrFromContext(c espejov1alpha1.Context) schema.GroupVersionResource {
	// obj := &corev1.Service{}
	// return obj.GroupVersionKind().GroupVersion().WithResource("service")

	// return schema.GroupVersionResource{
	// 	Group:    "",
	// 	Version:  "v1",
	// 	Resource: "namespace",
	// }

	split := strings.Split(c.APIVersion, "/")
	group := ""
	version := c.APIVersion
	if len(split) > 1 {
		group = split[0]
		version = split[1]
	}

	return schema.GroupVersionResource{
		Group:    group,
		Version:  version,
		Resource: strings.ToLower(c.Kind),
	}
}

// getContextMap returns a map with all resources from the context, with key = Alias.
func (p *Parser) getContextMap(ctx context.Context, clt *dynamic.Client) (map[string]*unstructured.UnstructuredList, error) {
	cm := make(map[string]*unstructured.UnstructuredList, 0)
	// for _, item := range p.Context {
	// 	list := &unstructured.UnstructuredList{}
	// 	for _, ns := range p.namespaceList {
	// 		l, err := clt.List(ctx, p.gvrFromContext(item), ns, v1.ListOptions{})
	// 		if err != nil {
	// 			return nil, err
	// 		}
	// 		list.Items = append(list.Items, l.Items...)
	// 	}
	// 	cm[item.Alias] = list
	// }

	return cm, nil
}

*/
