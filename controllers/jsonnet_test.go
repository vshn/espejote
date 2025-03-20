package controllers_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/google/go-jsonnet"
	"github.com/stretchr/testify/require"

	"github.com/vshn/espejote/controllers"
)

func Test_MultiImporter_Import_WithTrimPrefix(t *testing.T) {
	t.Parallel()

	subject := &controllers.MultiImporter{
		Importers: []controllers.MultiImporterConfig{
			{
				Importer: &jsonnet.MemoryImporter{
					Data: map[string]jsonnet.Contents{
						"test.jsonnet": jsonnet.MakeContents(`"test"`),
					},
				},
			},
			{
				TrimPathPrefix: "test/",
				Importer: &jsonnet.MemoryImporter{
					Data: map[string]jsonnet.Contents{
						"test.jsonnet": jsonnet.MakeContents(`"test/test"`),
					},
				},
			},
		},
	}
	jvm := jsonnet.MakeVM()
	jvm.Importer(subject)

	ret, err := jvm.EvaluateAnonymousSnippet("test.jsonnet", `[import "test.jsonnet", import "test/test.jsonnet"]`)
	require.NoError(t, err, "Content should be unique for each returned foundAt path, MultiImporter should re-add the TrimPathPrefix to the foundAt path or it will conflict with other imports")
	require.JSONEq(t, `["test", "test/test"]`, ret)

	slices.Collect(maps.Keys(subject.Cache))
	require.ElementsMatch(t, []string{"test.jsonnet", "test/test.jsonnet"}, slices.Collect(maps.Keys(subject.Cache)))
}
