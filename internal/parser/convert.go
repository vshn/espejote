package parser

import (
	"context"
	"regexp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

type Converter interface {
	// GetParserName return the name used for this parser.
	GetParserName() string

	// GetParserSepc returns parser from API type.
	GetParserSepc() espejov1alpha1.ParserSpec

	// GetNamespaceSelector returns namespace selector from API type.
	GetNamespaceSelector() espejov1alpha1.NamespaceSelector

	// IsNamespaced returns true if its a namespaced API type.
	IsNamespaced() bool
}

func Convert(ctx context.Context, c Converter, clt client.Client) (*Parser, error) {
	parser := &Parser{
		ParserSpec: c.GetParserSepc(),
		Name:       c.GetParserName(),
	}

	if c.IsNamespaced() {
		parser.namespaceList = c.GetNamespaceSelector().MatchNames
		return parser, nil
	}

	all, err := getAllNamespaces(ctx, clt)
	if err != nil {
		return nil, err
	}
	labeled, err := getLabeledNamespaces(ctx, clt, c.GetNamespaceSelector().LabelSelector)
	if err != nil {
		return nil, err
	}

	filtered := filterIgnoredNamespaces(c.GetNamespaceSelector().IgnoreNames, all)
	filtered = markMatchedNamespaces(labeled, filtered)
	filtered = markMatchedNamespaces(c.GetNamespaceSelector().MatchNames, filtered)

	parser.namespaceList = listFromMarked(filtered)

	return parser, nil
}

func getAllNamespaces(ctx context.Context, clt client.Client) ([]string, error) {
	list := &corev1.NamespaceList{}
	if err := clt.List(ctx, list, &client.ListOptions{}); err != nil {
		return nil, err
	}

	all := []string{}
	for _, ns := range list.Items {
		all = append(all, ns.Name)
	}

	return all, nil
}

func getLabeledNamespaces(ctx context.Context, clt client.Client, ls *metav1.LabelSelector) ([]string, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		return nil, err
	}

	list := &corev1.NamespaceList{}
	if err := clt.List(ctx, list, &client.ListOptions{
		LabelSelector: labelSelector,
	}); err != nil {
		return nil, err
	}

	all := []string{}
	for _, ns := range list.Items {
		all = append(all, ns.Name)
	}

	return all, nil
}

func filterIgnoredNamespaces(ignored, from []string) map[string]bool {
	filtered := make(map[string]bool)
	for _, name := range from {
		for _, expr := range ignored {
			rx := regexp.MustCompile(expr)
			if rx.MatchString(name) {
				continue
			}

			// Add to filtered list but mark as false
			filtered[name] = false
		}
	}

	return filtered
}

func markMatchedNamespaces(matches []string, in map[string]bool) map[string]bool {
	for name, _ := range in {
		for _, expr := range matches {
			rx := regexp.MustCompile(expr)
			if rx.MatchString(name) {
				continue
			}

			// Add to filtered list but mark as false
			in[name] = true
		}
	}

	return in
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
