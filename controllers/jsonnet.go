package controllers

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/google/go-jsonnet"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

//go:embed lib/espejote.libsonnet
var EspejoteLibsonnet string

// ImporterCacheEntry is a cache entry for MultiImporter.
// It holds the result of the last import.
type ImporterCacheEntry struct {
	Contents jsonnet.Contents
	FoundAt  string
	Err      error
}

// ManifestImporter imports data from espejotev1alpha1.JsonnetLibraries.
type ManifestImporter struct {
	client.Client

	Namespace        string
	LibraryNamespace string

	Cache map[string]ImporterCacheEntry
}

var espejoteLibsonnetContents = jsonnet.MakeContents(EspejoteLibsonnet)

// Import know about the topology of the espejote imports.
// It handles relative imports and absolute imports.
// It also caches the results of the imports.
// The cache is non thread-safe, same as the upstream file importer.
// The import "espejote.libsonnet" is statically embedded and always resolves to the built-in library.
// If a local "espejote.libsonnet" import is required it can be prefixed with "./".
// Relative imports by JsonnetLibraries always resolve to the same local or library namespace.
func (im *ManifestImporter) Import(importedFrom, importPath string) (contents jsonnet.Contents, foundAt string, err error) {
	if importPath == "espejote.libsonnet" {
		return espejoteLibsonnetContents, importPath, nil
	}

	if im.Cache == nil {
		im.Cache = make(map[string]ImporterCacheEntry)
	}

	absoluteImportPath := MakeAbsoluteImportPath(importedFrom, importPath)

	if cachedPV, ok := im.Cache[absoluteImportPath]; ok {
		return cachedPV.Contents, cachedPV.FoundAt, cachedPV.Err
	}

	c, foundAt, err := im.loadFromCluster(absoluteImportPath)
	if err != nil {
		err = fmt.Errorf("%w (import %q from %q absolute %q)", err, importPath, importedFrom, absoluteImportPath)
	}
	im.Cache[absoluteImportPath] = ImporterCacheEntry{
		Contents: c,
		FoundAt:  foundAt,
		Err:      err,
	}
	return c, foundAt, err
}

// MakeAbsoluteImportPath makes an absolute import path from the given importedFrom and importPath.
// It handles relative imports and absolute imports and knows about the topology of the espejote imports.
func MakeAbsoluteImportPath(importedFrom, importPath string) string {
	importPath = strings.TrimPrefix(importPath, "./")
	if strings.Count(importPath, "/") == 0 {
		dir, _ := splitLastSegment(importedFrom)
		return dir + "/" + importPath
	} else if strings.Count(importPath, "/") == 1 && strings.HasPrefix(importedFrom, "lib/") {
		return "lib/" + importPath
	}
	return importPath
}

func (im *ManifestImporter) loadFromCluster(fullImportPath string) (jsonnet.Contents, string, error) {
	segments := strings.Split(fullImportPath, "/")
	lib := false
	var manifestName, key string
	if len(segments) == 2 {
		manifestName, key = segments[0], segments[1]
	} else if len(segments) == 3 && segments[0] == "lib" {
		manifestName, key = segments[1], segments[2]
		lib = true
	} else {
		return jsonnet.Contents{}, "", fmt.Errorf("import %q not available: invalid path", fullImportPath)
	}

	namespace := im.Namespace
	if lib {
		namespace = im.LibraryNamespace
	}
	var library espejotev1alpha1.JsonnetLibrary
	if err := im.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: manifestName}, &library); err != nil {
		return jsonnet.Contents{}, "", fmt.Errorf("import %q not available: %w", fullImportPath, err)
	}

	if content, ok := library.Spec.Data[key]; ok {
		return jsonnet.MakeContents(content), fullImportPath, nil
	}
	return jsonnet.Contents{}, "", fmt.Errorf("import %q not available: key %q not found in manifest %q", fullImportPath, key, manifestName)
}

func splitLastSegment(path string) (string, string) {
	li := strings.LastIndex(path, "/")
	if li == -1 {
		return "", path
	}
	return path[:li], path[li+1:]
}

// FromClientImporter returns an importer that fetches libraries from the cluster using the given client.
// "espejote.libsonnet" is statically embedded.
func FromClientImporter(c client.Client, localNamespace, libNamespace string) *ManifestImporter {
	return &ManifestImporter{
		Client:           c,
		Namespace:        localNamespace,
		LibraryNamespace: libNamespace,
	}
}
