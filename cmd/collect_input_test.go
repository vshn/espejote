package cmd

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vshn/espejote/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

func Test_runCollectInput(t *testing.T) {
	t.Parallel()

	scheme, restCfg := testutil.SetupEnvtestEnv(t)

	cli, err := client.New(restCfg, client.Options{
		Scheme: scheme,
	})
	require.NoError(t, err)

	for _, file := range []string{
		"testdata/collect_inputs/managed_resource.yaml",
		"testdata/collect_inputs/managed_resource_referenced_context.yaml",
	} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()

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
			lib := &espejotev1alpha1.JsonnetLibrary{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "jsonnetlibrary-sample",
					Namespace: testns,
				},
				Spec: espejotev1alpha1.JsonnetLibrarySpec{
					Data: map[string]string{
						"sample.libsonnet": `{test: "test"}`,
					},
				},
			}
			require.NoError(t, cli.Create(t.Context(), lib))

			out := new(bytes.Buffer)
			cmd := NewCollectInputCommand(func() (*rest.Config, error) { return restCfg, nil })
			cmd.SetArgs([]string{file, "-n", testns})
			cmd.SetOut(out)
			require.NoError(t, cmd.Execute())

			t.Log(out.String())

			var ri RenderInput
			require.NoError(t, yaml.Unmarshal(out.Bytes(), &ri))

			var collectedTriggers []string
			for _, trigger := range ri.Triggers {
				resourceName := ""
				managedFields := "0"
				if trigger.WatchResource != nil {
					resourceName = trigger.WatchResource.GetName()
					managedFields = strconv.Itoa(len(trigger.WatchResource.GetManagedFields()))
				}
				collectedTriggers = append(collectedTriggers, strings.Join([]string{trigger.Name, resourceName, managedFields}, ":"))
			}
			var collectedContexts []string
			for _, context := range ri.Context {
				for _, resource := range context.Resources {
					collectedContexts = append(collectedContexts, strings.Join([]string{context.Name, resource.GetName(), strconv.Itoa(len(resource.GetManagedFields()))}, ":"))
				}
			}

			require.Equal(t, []string{"::0", "configmap:test-1:0", "configmap:test-5:0", "configmap:test-7:0", "configmap:test-9:0"}, collectedTriggers)
			require.Equal(t, []string{"configmaps:test-1:0", "configmaps:test-5:0", "configmaps:test-7:0", "configmaps:test-9:0"}, collectedContexts)
			require.Equal(t, map[string]string{"jsonnetlibrary-sample/sample.libsonnet": lib.Spec.Data["sample.libsonnet"]}, ri.Libraries)
		})
	}
}
