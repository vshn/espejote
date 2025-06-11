package plugins

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stealthrocket/wasi-go/imports"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/sys"
	"go.uber.org/multierr"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/oci"
	oraserrors "oras.land/oras-go/v2/errdef"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"vitess.io/vitess/go/pools"
)

type Manager struct {
	pluginPoolMutex sync.RWMutex
	pluginPool      map[string]func() (*pools.ResourcePool, error)

	pluginStore *oci.Store

	registry Registry
}

type Registry interface {
	Repository(registry.Reference) (oras.ReadOnlyTarget, error)
}

type RemoteRegistry struct{}

func (RemoteRegistry) Repository(ref registry.Reference) (oras.ReadOnlyTarget, error) {
	src, err := remote.NewRepository(strings.Join([]string{ref.Registry, ref.Repository}, "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to create remote repository for %q: %w", ref.String(), err)
	}
	return src, nil
}

func NewManager(dir string) (*Manager, error) {
	return NewManagerWithRegistry(dir, RemoteRegistry{})
}

func NewManagerWithRegistry(dir string, registry Registry) (*Manager, error) {
	pluginStore, err := oci.NewWithContext(context.TODO(), dir)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCI store: %w", err)
	}

	return &Manager{
		pluginPool:  make(map[string]func() (*pools.ResourcePool, error)),
		pluginStore: pluginStore,
		registry:    registry,
	}, nil
}

type runner struct {
	pluginName string
	ref        string
	digest     string

	// Used to expire the runner if the underlying plugin changes.
	myHandle      v1.Descriptor
	currentHandle atomic.Value

	runtime        wazero.Runtime
	compiledModule wazero.CompiledModule

	creationTime time.Time
}

func (r *runner) Close() {
	if r.runtime != nil {
		r.runtime.Close(context.Background())
	}
	if r.compiledModule != nil {
		r.compiledModule.Close(context.Background())
	}
}

func (r *runner) Expired(lifetimeTimeout time.Duration) bool {
	if lifetimeTimeout > 0 && time.Until(r.creationTime.Add(lifetimeTimeout)) < 0 {
		fmt.Println("Plugin runner expired due to lifetime timeout")
		return true
	}
	if !content.Equal(r.myHandle, r.currentHandle.Load().(v1.Descriptor)) {
		fmt.Println("Plugin runner expired due to plugin change")
		return true
	}
	return false
}

func (r *runner) Run(ctx context.Context, args []string) (string, string, error) {
	l := log.FromContext(ctx).WithName("PluginManager.runner.Run").WithValues("plugin", r.pluginName, "ref", r.ref, "digest", r.digest)
	l.Info("Running plugin", "args", args)
	startTime := time.Now()
	defer func() {
		l.Info("Ran plugin in", "duration", time.Since(startTime))
	}()

	// wasi-go supports sockets but does not support virtual filesystems.
	// We need to map the `/etc` directory to the `/etc` directory in the host filesystem for:
	// - `/etc/resolv.conf` to be available
	// - certificates to be available for TLS connections. See controllers/cert_locations.go for details.
	env := []string{
		fmt.Sprintf("%s=%s", CertDirEnv, strings.Join(CertDirectories, ":")),
	}
	if cf, ok := FindFirstCertFile(); ok {
		env = append(env, fmt.Sprintf("%s=%s", CertFileEnv, cf))
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	defer stdoutR.Close()
	defer io.Copy(io.Discard, stdoutR)
	defer stdoutW.Close()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	defer stderrR.Close()
	defer io.Copy(io.Discard, stderrR)
	defer stderrW.Close()

	var stdioWG sync.WaitGroup
	var stdout, stderr []byte
	var stdoutErr, stderrErr error
	stdioWG.Add(2)
	go func() {
		defer stdioWG.Done()
		stdout, stdoutErr = io.ReadAll(stdoutR)
	}()
	go func() {
		defer stdioWG.Done()
		stderr, stderrErr = io.ReadAll(stderrR)
	}()

	builder := imports.NewBuilder().
		WithStdio(-1, int(stdoutW.Fd()), int(stderrW.Fd())).
		WithArgs(args...).
		WithEnv(env...).
		WithDirs("/etc").
		WithSocketsExtension("auto", r.compiledModule)

	ctx, system, err := builder.Instantiate(ctx, r.runtime)
	if err != nil {
		return "", "", fmt.Errorf("failed to instantiate WASI system: %w", err)
	}

	instance, runErr := r.runtime.InstantiateModule(ctx, r.compiledModule, wazero.NewModuleConfig())
	var exitErr *sys.ExitError
	var isExitError bool
	if runErr != nil {
		if errors.As(runErr, &exitErr) {
			isExitError = true
			runErr = nil
		}
	}
	var icerr error
	if instance != nil {
		icerr = instance.Close(ctx)
	}
	scerr := system.Close(ctx)
	if err := multierr.Combine(runErr, icerr, scerr); err != nil {
		return "", "", fmt.Errorf("failed to run plugin %q: %w", r.pluginName, runErr)
	}

	// WASM does not manage stdout/stderr, so we need to close the pipes after the plugin is done.
	if err := stdoutW.Close(); err != nil {
		return "", "", fmt.Errorf("failed to close stdout pipe: %w", err)
	}
	if err := stderrW.Close(); err != nil {
		return "", "", fmt.Errorf("failed to close stderr pipe: %w", err)
	}

	stdioWG.Wait()
	if stdoutErr != nil {
		return "", "", fmt.Errorf("failed to read stdout: %w", stdoutErr)
	}
	if stderrErr != nil {
		return "", "", fmt.Errorf("failed to read stderr: %w", stderrErr)
	}

	var exitCode int
	if isExitError {
		exitCode = int(exitErr.ExitCode())
		l.Info("Plugin output", "exitCode", exitCode)
		return string(stdout), string(stderr), exitErr
	}

	return string(stdout), string(stderr), nil
}

func (m *Manager) RunPlugin(ctx context.Context, name string, args []string) (string, string, error) {
	m.pluginPoolMutex.RLock()
	getPool, ok := m.pluginPool[name]
	m.pluginPoolMutex.RUnlock()
	if !ok {
		return "", "", fmt.Errorf("plugin %q is not registered", name)
	}

	pool, err := getPool()
	if err != nil {
		return "", "", fmt.Errorf("failed to get plugin pool for %q: %w", name, err)
	}

	resource, err := pool.Get(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to get plugin resource from pool for %q: %w", name, err)
	}
	defer pool.Put(resource)

	runnerInstance := resource.(*runner)
	return runnerInstance.Run(ctx, args)
}

var ErrAlreadyRegistered = errors.New("plugin is already registered")

func (m *Manager) RegisterPlugin(ctx context.Context, name, ref string) error {
	m.pluginPoolMutex.Lock()
	if p := m.pluginPool[name]; p != nil {
		defer m.pluginPoolMutex.Unlock()
		return fmt.Errorf("error registering plugin %q: %w", name, ErrAlreadyRegistered)
	}

	m.pluginPool[name] = sync.OnceValues(func() (*pools.ResourcePool, error) {
		var pluginStorageHandle atomic.Value
		psh, err := m.ensurePlugin(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to ensure plugin %q: %w", ref, err)
		}
		pluginStorageHandle.Store(psh)

		factory := func(ctx context.Context) (pools.Resource, error) {
			handle := pluginStorageHandle.Load().(v1.Descriptor)

			l := log.FromContext(ctx).WithName("resourcePool.factory").WithValues("plugin", name, "ref", ref, "digest", handle.Digest.String())
			l.Info("Compiling plugin")
			startTime := time.Now()
			defer func() {
				l.Info("Compiled plugin", "duration", time.Since(startTime))
			}()

			pluginContentReader, err := m.pluginStore.Fetch(ctx, handle)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch plugin content for %q: %w", ref, err)
			}
			defer pluginContentReader.Close()
			defer io.Copy(io.Discard, pluginContentReader)
			pluginContent, err := content.ReadAll(pluginContentReader, handle)
			if err != nil {
				return nil, fmt.Errorf("failed to read plugin content for %q: %w", ref, err)
			}

			runtime := wazero.NewRuntime(ctx)

			wasmModule, err := runtime.CompileModule(ctx, pluginContent)
			if err != nil {
				return nil, fmt.Errorf("failed to compile WASM module for plugin %q: %w", ref, err)
			}
			return &runner{
				pluginName: name,
				ref:        ref,
				digest:     handle.Digest.String(),

				myHandle:      handle,
				currentHandle: pluginStorageHandle,

				runtime:        runtime,
				compiledModule: wasmModule,
				creationTime:   time.Now(),
			}, nil
		}

		refreshInterval := 5 * time.Minute

		pool := pools.NewResourcePool(
			factory,
			10,
			10,
			5*time.Minute,
			time.Hour,
			func(waitStart time.Time) {
				fmt.Printf("Pool %q waiting for %s\n", name, time.Since(waitStart))
			},
			func() (bool, error) {
				l := log.Log.WithName("pluginPool.refresh").WithValues("plugin", name, "ref", ref)
				l.Info("Background refresh of plugin", "interval", refreshInterval)
				ctx, cancel := context.WithTimeout(log.IntoContext(context.Background(), l), refreshInterval)
				defer cancel()

				psh, err := m.ensurePlugin(ctx, ref)
				if err != nil {
					return false, fmt.Errorf("failed to ensure plugin %q: %w", ref, err)
				}
				if content.Equal(psh, pluginStorageHandle.Load().(v1.Descriptor)) {
					return false, nil // No change in the plugin, no need to update.
				}
				l.Info("Plugin updated")
				pluginStorageHandle.Store(psh)
				return true, nil
			},
			refreshInterval,
		)
		return pool, nil
	})
	m.pluginPoolMutex.Unlock()

	// Initialize the plugin pool to ensure the plugin is downloaded and available.
	_, err := m.pluginPool[name]()
	return err
}

// ensurePlugin ensures that the plugin with the given URL is downloaded and available in the local store.
// It returns the descriptor of the plugin's WASM layer.
func (m *Manager) ensurePlugin(ctx context.Context, url string) (v1.Descriptor, error) {
	l := log.FromContext(ctx).WithName("PluginManager.ensurePlugin").WithValues("ref", url)

	pluginRef, err := registry.ParseReference(url)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("failed to parse plugin reference %q: %w", url, err)
	}
	pluginRef.Reference = pluginRef.ReferenceOrDefault()

	src, err := m.registry.Repository(pluginRef)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("failed to create remote repository for %q: %w", url, err)
	}

	srcDesc, err := src.Resolve(ctx, pluginRef.Reference)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("failed to resolve plugin in remote repository %q: %w", url, err)
	}

	cacheDesc, cacheDescErr := m.pluginStore.Resolve(ctx, pluginRef.String())
	if cacheDescErr != nil && !errors.Is(cacheDescErr, oraserrors.ErrNotFound) {
		return v1.Descriptor{}, fmt.Errorf("failed to resolve plugin in local store %q: %w", url, cacheDescErr)
	} else if errors.Is(cacheDescErr, oraserrors.ErrNotFound) || !content.Equal(srcDesc, cacheDesc) {
		l.Info("Downloading plugin", "url", url, "digest", srcDesc.Digest)
		startTime := time.Now()
		desc, err := oras.Copy(ctx, src, pluginRef.Reference, m.pluginStore, pluginRef.String(), oras.DefaultCopyOptions)
		if err != nil {
			return v1.Descriptor{}, fmt.Errorf("failed to download plugin %q to local store: %w", url, err)
		}
		l.Info("Downloaded new version of plugin", "url", url, "digest", desc.Digest, "duration", time.Since(startTime))

		cacheDesc = desc
	} else {
		l.Info("Using cached plugin", "url", url, "digest", cacheDesc.Digest)
	}

	ss, err := content.Successors(ctx, m.pluginStore, cacheDesc)
	if err != nil {
		return v1.Descriptor{}, fmt.Errorf("failed to get layers from the plugin store %q: %w", url, err)
	}
	const wasmContentLayerMediaType = "application/vnd.module.wasm.content.layer.v1+wasm"
	var wasmLayerDesc v1.Descriptor
	var foundWASMLayerDesc bool
	for _, s := range ss {
		if s.MediaType == wasmContentLayerMediaType {
			foundWASMLayerDesc = true
			wasmLayerDesc = s
			break
		}
	}
	if !foundWASMLayerDesc {
		return v1.Descriptor{}, fmt.Errorf("no %q layer found in the plugin %q", wasmContentLayerMediaType, url)
	}

	return wasmLayerDesc, nil
}
