package controllers

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/google/go-jsonnet"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

//go:embed lib/espejote.libsonnet
var EspejoteLibsonnet string

// MultiImporter imports from multiple importers.
// It tries each importer in order until one succeeds.
// If all importers fail, it returns an error combining all errors.
// Lookup results are cached for the lifetime of the importer and can be accessed through the Cache field.
// The cache is not thread-safe. Same as the upstream FileImporter.
type MultiImporter struct {
	Importers []MultiImporterConfig

	Cache map[string]MultiImporterCacheEntry
}

// MultiImporterCacheEntry is a cache entry for MultiImporter.
// It holds the result of the last import.
type MultiImporterCacheEntry struct {
	Contents jsonnet.Contents
	FoundAt  string
	Err      error
}

type MultiImporterConfig struct {
	Importer       jsonnet.Importer
	TrimPathPrefix string
}

// Import fetches from the first importer that succeeds.
func (im *MultiImporter) Import(importedFrom, importedPath string) (contents jsonnet.Contents, foundAt string, err error) {
	if im.Cache == nil {
		im.Cache = make(map[string]MultiImporterCacheEntry)
	}
	if entry, ok := im.Cache[importedPath]; ok {
		return entry.Contents, entry.FoundAt, entry.Err
	}

	var errs []error
	for _, i := range im.Importers {
		path := importedPath
		if i.TrimPathPrefix != "" {
			if !strings.HasPrefix(path, i.TrimPathPrefix) {
				continue
			}
			path = path[len(i.TrimPathPrefix):]
		}
		contents, foundAt, err := i.Importer.Import(importedFrom, path)
		if i.TrimPathPrefix != "" {
			foundAt = i.TrimPathPrefix + foundAt
		}
		if err == nil {
			im.Cache[importedPath] = MultiImporterCacheEntry{
				Contents: contents,
				FoundAt:  foundAt,
				Err:      nil,
			}
			return contents, foundAt, nil
		}
		errs = append(errs, err)
	}

	err = fmt.Errorf("import not available %q: %w", importedPath, multierr.Combine(errs...))
	im.Cache[importedPath] = MultiImporterCacheEntry{
		Contents: jsonnet.Contents{},
		FoundAt:  "",
		Err:      err,
	}
	return jsonnet.Contents{}, "", err
}

// ManifestImporter imports data from espejotev1alpha1.JsonnetLibraries.
type ManifestImporter struct {
	client.Client

	Namespace string
}

// Import fetches from espejotev1alpha1.JsonnetLibraries in the cluster
// The first path segment is the name of the JsonnetLibrary object.
// The second path segment is the key in the JsonnetLibrary object.
func (importer *ManifestImporter) Import(_, importedPath string) (contents jsonnet.Contents, foundAt string, err error) {
	segments := strings.SplitN(importedPath, "/", 2)
	if len(segments) != 2 {
		return jsonnet.Contents{}, "", fmt.Errorf("invalid import path %v", importedPath)
	}
	manifestName, key := segments[0], segments[1]

	var library espejotev1alpha1.JsonnetLibrary
	if err := importer.Get(context.Background(), types.NamespacedName{Namespace: importer.Namespace, Name: manifestName}, &library); err != nil {
		return jsonnet.Contents{}, "", fmt.Errorf("import %q not available: %w", importedPath, err)
	}

	if content, ok := library.Spec.Data[key]; ok {
		return jsonnet.MakeContents(content), importedPath, nil
	}
	return jsonnet.Contents{}, "", fmt.Errorf("import %q not available: key %q not found in manifest %q", importedPath, key, manifestName)
}

// FromClientImporter returns an importer that fetches libraries from the cluster using the given client.
// "espejote.libsonnet" is statically embedded.
func FromClientImporter(c client.Client, localNamespace, libNamespace string) *MultiImporter {
	return &MultiImporter{
		Importers: []MultiImporterConfig{
			{
				Importer: &jsonnet.MemoryImporter{
					Data: map[string]jsonnet.Contents{
						"espejote.libsonnet": jsonnet.MakeContents(EspejoteLibsonnet),
					},
				},
			},
			{
				TrimPathPrefix: "lib/",
				Importer: &ManifestImporter{
					Client:    c,
					Namespace: libNamespace,
				},
			},
			{
				Importer: &ManifestImporter{
					Client:    c,
					Namespace: localNamespace,
				},
			},
		},
	}
}
