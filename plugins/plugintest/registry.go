package plugintest

import (
	"fmt"
	"sync"
	"testing"

	ociimagev1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/file"
	"oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/registry"
)

type MockPluginRegistry struct {
	t *testing.T

	registryMux sync.Mutex
	registry    map[string]func() (*oci.Store, error)
}

// NewMockPluginRegistry creates a new instance of MockPluginRegistry for testing purposes.
// It retains a reference to the testing.T object for context management.
// The registry is no longer usable after the test completes.
func NewMockPluginRegistry(t *testing.T) *MockPluginRegistry {
	return &MockPluginRegistry{
		t:        t,
		registry: make(map[string]func() (*oci.Store, error)),
	}
}

// Repository returns a repository for the given reference.
func (m *MockPluginRegistry) Repository(ref registry.Reference) (oras.ReadOnlyTarget, error) {
	return m.store(ref)
}

func (m *MockPluginRegistry) store(ref registry.Reference) (*oci.Store, error) {
	name := fmt.Sprintf("%s/%s", ref.Registry, ref.Repository)

	m.registryMux.Lock()
	if _, ok := m.registry[name]; !ok {
		m.registry[name] = sync.OnceValues(func() (*oci.Store, error) {
			return oci.NewWithContext(m.t.Context(), m.t.TempDir())
		})
	}
	m.registryMux.Unlock()

	return m.registry[name]()
}

// UploadFile uploads a file to the mock plugin registry.
// It sets the WASM content type.
// The uploaded file can be retrieved using
//
//	`Repository(ref).Resolve(ref.Reference)`
func (m *MockPluginRegistry) UploadFile(path string, as registry.Reference) error {
	const wasmContentType = "application/vnd.module.wasm.content.layer.v1+wasm"
	const localPackRef = "latest"

	fs, err := file.New("")
	if err != nil {
		return fmt.Errorf("failed to create file store before upload: %w", err)
	}
	defer fs.Close()
	pd, err := fs.Add(m.t.Context(), path, wasmContentType, "")
	if err != nil {
		return fmt.Errorf("failed to add file %q to store before upload: %w", path, err)
	}

	opts := oras.PackManifestOptions{
		Layers: []ociimagev1.Descriptor{pd},
	}
	manifestDescriptor, err := oras.PackManifest(m.t.Context(), fs, oras.PackManifestVersion1_1, wasmContentType, opts)
	if err != nil {
		return fmt.Errorf("failed to pack manifest before upload: %w", err)
	}
	if err := fs.Tag(m.t.Context(), manifestDescriptor, localPackRef); err != nil {
		return fmt.Errorf("failed to tag manifest before upload: %w", err)
	}

	repo, err := m.store(as)
	if err != nil {
		return fmt.Errorf("failed to get repository for reference %q: %w", as, err)
	}

	_, err = oras.Copy(m.t.Context(), fs, localPackRef, repo, as.ReferenceOrDefault(), oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("failed to copy file to repository %q: %w", as, err)
	}

	return nil
}
