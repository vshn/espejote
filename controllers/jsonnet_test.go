package controllers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/controllers"
)

func Test_ManifestImporter_Import_NoLocalNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, espejotev1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&espejotev1alpha1.JsonnetLibrary{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared",
					Namespace: "lib-ns",
				},
				Spec: espejotev1alpha1.JsonnetLibrarySpec{
					Data: map[string]string{
						"test.libsonnet": `{}`,
					},
				},
			},
		).
		Build()

	subject := controllers.FromClientImporter(c, "", "lib-ns")

	_, _, err := subject.Import("template", "local/config.libsonnet")
	assert.ErrorContains(t, err, "local/config.libsonnet")
	assert.ErrorContains(t, err, "no local namespace configured")

	_, _, err = subject.Import("template", "espejote.libsonnet")
	assert.NoError(t, err, "embedded import should work without local namespace")
	contents, _, err := subject.Import("template", "lib/shared/test.libsonnet")
	assert.NoError(t, err, "shared library import should work without local namespace")
	assert.Equal(t, "{}", contents.String())
}
