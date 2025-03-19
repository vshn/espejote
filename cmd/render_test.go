package cmd

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func Test_runRender_InputFile(t *testing.T) {
	t.Parallel()

	out := new(bytes.Buffer)

	cmd := NewRenderCommand()
	cmd.SetArgs([]string{"testdata/render_inputs/managed_resource.yaml", "--input", "testdata/render_inputs/input1.yaml"})
	cmd.SetOut(out)
	require.NoError(t, cmd.Execute())

	// Regenerate with:
	// $ go run . render cmd/testdata/render_inputs/managed_resource.yaml --input cmd/testdata/render_inputs/input1.yaml > cmd/testdata/render_inputs/input1_golden.yaml
	expected := requireReadFile(t, "testdata/render_inputs/input1_golden.yaml")
	require.Equal(t, expected, out.String())
}

func requireReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func Test_staticReader(t *testing.T) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "example.com",
		Version: "v1",
		Kind:    "Example",
	})
	obj.SetName("example")
	obj.SetNamespace("default")

	subject := staticUnstructuredReader{
		objs: []*unstructured.Unstructured{obj},
	}

	var got unstructured.Unstructured
	require.NoError(t, subject.Get(t.Context(), types.NamespacedName{}, &got))
	require.Equal(t, *obj, got)

	var gotList unstructured.UnstructuredList
	require.NoError(t, subject.List(t.Context(), &gotList))
	require.Len(t, gotList.Items, 1)
	require.Equal(t, *obj, gotList.Items[0])
}
