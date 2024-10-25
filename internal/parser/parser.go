package parser

import (
	"bytes"
	"context"
	"strings"

	"github.com/google/go-jsonnet"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/internal/dynamic"
)

type Parser struct {
	Name     string
	Input    map[string]*unstructured.UnstructuredList
	Template string
}

var serializer = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

// Execute runs the Jsonnet snippet with external-variables.
func (p *Parser) Parse(ctx context.Context, clt *dynamic.Client) error {
	// Create jsonnet VM and add external variables.
	vm := jsonnet.MakeVM()

	cm, err := p.getContextMap(ctx, clt)
	if err != nil {
		return err
	}

	// Add context item as external variable named from alias.
	for k, v := range cm {
		data := bytes.NewBufferString("")
		if err := serializer.Encode(v, data); err != nil {
			return err
		}
		vm.ExtCode(k, data.String())
	}

	// Run Jsonnet snippet.
	output, err := vm.EvaluateAnonymousSnippet(p.Name, p.Template)
	if err != nil {
		return err
	}

	// Apply output.
	return clt.Apply(ctx, output)
}

// getContextMap returns a map with all resources from the context, with key = Alias.
func (p *Parser) getContextMap(ctx context.Context, clt *dynamic.Client) (map[string]*unstructured.UnstructuredList, error) {
	cm := make(map[string]*unstructured.UnstructuredList, 0)
	for _, item := range p.Context {
		list := &unstructured.UnstructuredList{}
		for _, ns := range p.namespaceList {
			l, err := clt.List(ctx, p.gvrFromContext(item), ns, v1.ListOptions{})
			if err != nil {
				return nil, err
			}
			list.Items = append(list.Items, l.Items...)
		}
		cm[item.Alias] = list
	}

	return cm, nil
}

// gvrFromContext returns the GroupVersionResource from the context item.
func (p *Parser) gvrFromContext(c espejov1alpha1.Context) schema.GroupVersionResource {
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
