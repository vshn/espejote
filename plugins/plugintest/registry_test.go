package plugintest_test

import (
	"io"
	"os"
	"path"
	"testing"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"github.com/vshn/espejote/plugins/plugintest"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
)

func Test_MockPluginRegistry(t *testing.T) {
	subject := plugintest.NewMockPluginRegistry(t)

	filename := path.Join(t.TempDir(), "test.wasm")

	const expectedTestFileContent = "test content"

	require.NoError(t, os.WriteFile(filename, []byte(expectedTestFileContent), 0644))

	ref := registry.Reference{Registry: "test.espejote.io", Repository: "test-repo", Reference: "test-ref"}
	require.NoError(t, subject.UploadFile(filename, ref))

	repo, err := subject.Repository(ref)
	require.NoError(t, err)
	manifest, err := repo.Resolve(t.Context(), ref.Reference)
	require.NoError(t, err)

	wasmLayerDesc := requireWASMSuccessor(t, repo, manifest)
	rc, err := repo.Fetch(t.Context(), wasmLayerDesc)
	require.NoError(t, err)
	defer rc.Close()

	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, expectedTestFileContent, string(content))
}

func requireWASMSuccessor(t *testing.T, repo oras.ReadOnlyTarget, manifest v1.Descriptor) v1.Descriptor {
	t.Helper()

	const wasmContentType = "application/vnd.module.wasm.content.layer.v1+wasm"

	ss, err := content.Successors(t.Context(), repo, manifest)
	require.NoError(t, err)

	for _, s := range ss {
		if s.MediaType == wasmContentType {
			return s
		}
	}

	t.Fatalf("No successor with media type %q found", wasmContentType)
	return v1.Descriptor{} // Unreachable, but required for compilation
}
