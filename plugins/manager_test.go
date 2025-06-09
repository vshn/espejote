package plugins_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero/sys"
	"github.com/vshn/espejote/plugins"
	"github.com/vshn/espejote/plugins/plugintest"
	"oras.land/oras-go/v2/registry"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

//go:generate env GOOS=wasip1 GOARCH=wasm go build -o ./testdata/failing/plugin.wasm ./testdata/failing
//go:generate env GOOS=wasip1 GOARCH=wasm go build -o ./testdata/http/plugin.wasm ./testdata/http
//go:generate env GOOS=wasip1 GOARCH=wasm go build -o ./testdata/static/plugin.wasm ./testdata/static

const httpTestCrumb = "crumb:e8FbcSYT3Ws5z23v5BH42uVufm3geljVgZUvgh3Y9EONOIBnpE"

func Test_Manager_RegisterAndRun(t *testing.T) {
	ctx := log.IntoContext(t.Context(), testr.New(t))

	type pluginTest struct {
		name          string
		args          []string
		skip          func() string // Skips the test if a reason is returned
		outputMatcher func(t *testing.T, stdout, stderr string, err error)
	}

	skip := func(pt pluginTest) string {
		if pt.skip == nil {
			return ""
		}
		return pt.skip()
	}

	pluginsToTest := []pluginTest{
		{
			name: "failing",
			outputMatcher: func(t *testing.T, stdout, stderr string, err error) {
				assert.ErrorIs(t, err, sys.NewExitError(2), "Expected sys.ExitError for plugin failing")
			},
		},
		{
			name: "http",
			skip: func() string {
				if testing.Short() {
					return "Skipping HTTP plugin test in short mode"
				}
				return ""
			},
			outputMatcher: func(t *testing.T, stdout, stderr string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, httpTestCrumb)
			},
		},
		{
			name: "static",
			args: []string{"Hello, World!"},
			outputMatcher: func(t *testing.T, stdout, stderr string, err error) {
				assert.NoError(t, err)
				assert.Contains(t, stdout, "Hello, World!")
				assert.Contains(t, stderr, "Hello, World!")
			},
		},
	}

	mockRegistry := plugintest.NewMockPluginRegistry(t)
	manager, err := plugins.NewManagerWithRegistry(t.TempDir(), mockRegistry)
	require.NoError(t, err)

	for _, plugin := range pluginsToTest {
		t.Run("upload:"+plugin.name, func(t *testing.T) {
			if skipReason := skip(plugin); skipReason != "" {
				t.Skipf("Skipping plugin %s: %s", plugin.name, skipReason)
			}
			ref := registry.Reference{
				Registry:   "test.espejote.io",
				Repository: plugin.name,
				Reference:  "latest",
			}
			require.NoError(t, mockRegistry.UploadFile(fmt.Sprintf("testdata/%s/plugin.wasm", plugin.name), ref))
		})
	}

	beforeRegister := time.Now()
	for _, plugin := range pluginsToTest {
		t.Run("register:"+plugin.name, func(t *testing.T) {
			if skipReason := skip(plugin); skipReason != "" {
				t.Skipf("Skipping plugin %s: %s", plugin.name, skipReason)
			}
			pluginRef := registry.Reference{
				Registry:   "test.espejote.io",
				Repository: plugin.name,
				Reference:  "latest",
			}
			require.NoError(t, manager.RegisterPlugin(ctx, pluginRef.Repository, pluginRef.String()))
			t.Logf("Registered plugin %s", plugin.name)
		})
	}
	t.Logf("Registering plugins took %s", time.Since(beforeRegister))

	afterRegister := time.Now()
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(len(pluginsToTest))
		for _, plugin := range pluginsToTest {
			go func() {
				defer wg.Done()
				if skipReason := skip(plugin); skipReason != "" {
					t.Logf("Skipping plugin %s: %s", plugin.name, skipReason)
					return
				}

				stdout, stderr, err := manager.RunPlugin(ctx, plugin.name, plugin.args)
				t.Logf("Ran plugin %s: stdout=%q, stderr=%q, err=%v", plugin.name, stdout, stderr, err)
				plugin.outputMatcher(t, stdout, stderr, err)
			}()
		}
	}
	wg.Wait()
	t.Logf("Running plugins took %s", time.Since(afterRegister))
}
