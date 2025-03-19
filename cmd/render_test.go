package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vshn/espejote/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_runRender_Cluster(t *testing.T) {
	scheme, restCfg := testutil.SetupEnvtestEnv(t)

	cli, err := client.New(restCfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	testns := testutil.TmpNamespace(t, cli)

	for i := range 10 {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-%d", i),
				Namespace: testns,
				Labels:    map[string]string{},
			},
		}
		if i%2 != 0 {
			cm.Labels["odd"] = "true"
		}
		require.NoError(t, cli.Create(t.Context(), cm))
	}

	out := new(bytes.Buffer)
	cmd := NewRenderCommand(func() (*rest.Config, error) { return restCfg, nil })
	cmd.SetArgs([]string{"testdata/render_cluster/managed_resource.yaml", "-n", testns})
	cmd.SetOut(out)
	require.NoError(t, cmd.Execute())

	expected := strings.ReplaceAll(requireReadFile(t, "testdata/render_cluster/golden.yaml"), "NAMESPACE", testns) // Replace the generated namespace name
	require.Equal(t, expected, out.String())
}

func Test_runRender_InputFile(t *testing.T) {
	t.Parallel()

	out := new(bytes.Buffer)
	cmd := NewRenderCommand(func() (*rest.Config, error) { return nil, errors.New("should not connect to cluster") })
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
