package parser

import (
	"bytes"
	"encoding/json"

	"github.com/google/go-jsonnet"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

type Parser struct {
	Name       string
	Input      map[string]*unstructured.UnstructuredList
	Template   string
	Namespaces *corev1.NamespaceList
}

var serializer = yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

// Execute runs the Jsonnet snippet with external-variables.
func (p *Parser) Parse() ([]*unstructured.Unstructured, error) {
	output, err := p.ParseToJson()
	if err != nil {
		return nil, err
	}

	// Convert to array of Unstructured objects.
	list := []*unstructured.Unstructured{}
	for _, o := range output {
		obj := &unstructured.Unstructured{}
		err := json.Unmarshal([]byte(o), obj)
		// obj, _, err := serializer.Decode([]byte(o), nil, nil)
		if err != nil {
			continue
		}
		list = append(list, obj)
	}

	return list, nil
}

func (p *Parser) ParseToJson() ([]string, error) {
	// Create jsonnet VM and add external variables.
	vm := jsonnet.MakeVM()

	// Add list of namespaces as external variable named namespaces.
	data := bytes.NewBufferString("")
	if err := serializer.Encode(p.Namespaces, data); err != nil {
		return nil, err
	}
	vm.ExtCode("namespaces", data.String())

	// Add input items as external variable named from alias.
	for k, v := range p.Input {
		data := bytes.NewBufferString("")
		if err := serializer.Encode(v, data); err != nil {
			return nil, err
		}
		vm.ExtCode(k, data.String())
	}

	// Run Jsonnet snippet.
	output, err := vm.EvaluateAnonymousSnippetStream(p.Name, p.Template)
	if err != nil {
		return nil, err
	}

	return output, nil
}
