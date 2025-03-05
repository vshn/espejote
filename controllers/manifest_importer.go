package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-jsonnet"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

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
		return jsonnet.Contents{}, "", fmt.Errorf("import not available %v", importedPath)
	}

	if content, ok := library.Spec.Data[key]; ok {
		return jsonnet.MakeContents(content), importedPath, nil
	}
	return jsonnet.Contents{}, "", fmt.Errorf("import not available %v", importedPath)
}
