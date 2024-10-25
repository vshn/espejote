package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Test_Simple_Template(t *testing.T) {

	p := Parser{
		Name:  "test-parser",
		Input: make(map[string]*unstructured.UnstructuredList, 0),
		Template: `[
 {
  apiVersion: 'monitoring.coreos.com/v1',
  kind: 'PrometheusRule',
  metadata: {
    name: 'prometheus-rule',
    namespace: 'monitoring',
  },
  spec: {},
},
]`,
	}

	is, err := p.Parse()

	assert.Empty(t, err)
	assert.Equal(t, "PrometheusRule", is[0].GetObjectKind().GroupVersionKind().Kind)
	// assert.Equal(t, "PrometheusRule", is.Items[0].GetKind())
}
