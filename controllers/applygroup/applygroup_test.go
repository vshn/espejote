package applygroup_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vshn/espejote/controllers/applygroup"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Test_UnmarshalJSON(t *testing.T) {
	abortGroupConf := mustMarshal(t, map[string]any{
		"apiVersion": "internal.espejote.io/v1",
		"kind":       "ApplyGroupConfig",
		"spec": map[string]any{
			"errorPolicy": "Abort",
		},
	})
	continueGroupConf := mustMarshal(t, map[string]any{
		"apiVersion": "internal.espejote.io/v1",
		"kind":       "ApplyGroupConfig",
		"spec": map[string]any{
			"errorPolicy": "Continue",
		},
	})

	for _, tc := range []struct {
		name     string
		defaults applygroup.ApplyDefaults
		json     string
		expected func(applygroup.ApplyDefaults) applygroup.Applier
	}{
		{
			name:     "null",
			defaults: applygroup.ApplyDefaults{},
			json:     `null`,
			expected: func(applygroup.ApplyDefaults) applygroup.Applier {
				return applygroup.Applier{
					Kind: applygroup.NoopKind,
				}
			},
		},
		{
			name: "regular object",
			defaults: applygroup.ApplyDefaults{
				FieldManagerFallback: "default-field-manager",
			},
			json: `{"kind":"Example"}`,
			expected: func(def applygroup.ApplyDefaults) applygroup.Applier {
				return applygroup.Applier{
					ApplyDefaults: def,
					Kind:          applygroup.ApplyKind,
					Resource:      mustUnmarshalUnstructured(t, `{"kind":"Example"}`),
					ResourceApplyOptions: []client.PatchOption{
						client.FieldValidation("Strict"),
						client.FieldOwner(def.FieldManagerFallback),
					},
				}
			},
		},
		{
			name: "delete object",
			defaults: applygroup.ApplyDefaults{
				FieldManagerFallback: "default-field-manager",
			},
			json: func() string {
				u := new(unstructured.Unstructured)
				u.SetKind("Example")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "Example"})
				injectDeleteOptions(t, u, "propagationPolicy", "Foreground")
				return mustMarshal(t, u)
			}(),
			expected: func(def applygroup.ApplyDefaults) applygroup.Applier {
				return applygroup.Applier{
					ApplyDefaults: def,
					Kind:          applygroup.DeleteKind,
					Resource:      mustUnmarshalUnstructured(t, `{"apiVersion":"example.com/v1","kind":"Example"}`),
					ResourceDeleteOptions: []client.DeleteOption{
						client.PropagationPolicy("Foreground"),
					},
				}
			},
		},
		{
			name:     "array (apply group)",
			defaults: applygroup.ApplyDefaults{},
			json:     `[]`,
			expected: func(def applygroup.ApplyDefaults) applygroup.Applier {
				return applygroup.Applier{
					ApplyDefaults: def,
					Kind:          applygroup.GroupKind,
				}
			},
		},
		{
			name: "nested",
			defaults: applygroup.ApplyDefaults{
				FieldManagerFallback: "default-field-manager",
			},
			json: `[[[]],{"kind":"Blubber"}]`,
			expected: func(def applygroup.ApplyDefaults) applygroup.Applier {
				outerMost := applygroup.Applier{
					Kind:          applygroup.GroupKind,
					ApplyDefaults: def,
				}
				level2 := applygroup.Applier{
					Parent:        &outerMost,
					Kind:          applygroup.GroupKind,
					ApplyDefaults: def,
				}
				level3 := applygroup.Applier{
					Parent:        &level2,
					Kind:          applygroup.GroupKind,
					ApplyDefaults: def,
				}
				level2.Group = []applygroup.Applier{level3}
				outerMost.Group = []applygroup.Applier{level2, {
					Parent:        &outerMost,
					Kind:          applygroup.ApplyKind,
					ApplyDefaults: def,
					Resource:      mustUnmarshalUnstructured(t, `{"kind":"Blubber"}`),
					ResourceApplyOptions: []client.PatchOption{
						client.FieldValidation("Strict"),
						client.FieldOwner("default-field-manager"),
					},
				}}
				return outerMost
			},
		},
		{
			name: "complex",
			defaults: applygroup.ApplyDefaults{
				FieldManagerFallback: "default-field-manager",
			},
			json: `[` + abortGroupConf + `, null, {"kind":"Example"},[],[` + continueGroupConf + `,{"kind":"OtherExample"}]]`,
			expected: func(def applygroup.ApplyDefaults) applygroup.Applier {
				outerMost := applygroup.Applier{
					Kind:          applygroup.GroupKind,
					ApplyDefaults: def,
					ErrorPolicy:   "Abort",
				}
				groupWithErrPolicyFromParent := applygroup.Applier{
					Parent:        &outerMost,
					Kind:          applygroup.GroupKind,
					ApplyDefaults: def,
					ErrorPolicy:   "Abort",
				}
				secondInnerGroup := applygroup.Applier{
					Parent:        &outerMost,
					Kind:          applygroup.GroupKind,
					ApplyDefaults: def,
					ErrorPolicy:   "Continue",
				}
				secondInnerGroup.Group = append(secondInnerGroup.Group, applygroup.Applier{
					Parent:        &secondInnerGroup,
					Kind:          applygroup.ApplyKind,
					ApplyDefaults: def,
					Resource:      mustUnmarshalUnstructured(t, `{"kind":"OtherExample"}`),
					ResourceApplyOptions: []client.PatchOption{
						client.FieldValidation("Strict"),
						client.FieldOwner("default-field-manager"),
					},
				})
				outerMost.Group = []applygroup.Applier{
					{
						Parent:        &outerMost,
						Kind:          applygroup.NoopKind,
						ApplyDefaults: def,
					},
					{
						Parent:        &outerMost,
						Kind:          applygroup.ApplyKind,
						ApplyDefaults: def,
						Resource:      mustUnmarshalUnstructured(t, `{"kind":"Example"}`),
						ResourceApplyOptions: []client.PatchOption{
							client.FieldValidation("Strict"),
							client.FieldOwner("default-field-manager"),
						},
					},
					groupWithErrPolicyFromParent,
					secondInnerGroup,
				}
				return outerMost
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := applygroup.Applier{ApplyDefaults: tc.defaults}
			require.NoError(t, json.Unmarshal([]byte(tc.json), &a))
			require.Equal(t, tc.expected(tc.defaults), a)
		})
	}
}

func mustMarshal(t *testing.T, a any) string {
	t.Helper()

	b, err := json.Marshal(a)
	require.NoError(t, err)
	return string(b)
}

func mustUnmarshalUnstructured(t *testing.T, s string) *unstructured.Unstructured {
	t.Helper()

	var u unstructured.Unstructured
	require.NoError(t, json.Unmarshal([]byte(s), &u))
	return &u
}

func injectDeleteOptions(t *testing.T, u *unstructured.Unstructured, key string, value any) *unstructured.Unstructured {
	t.Helper()

	require.NoError(t, unstructured.SetNestedField(u.Object, true, "__internal_use_espejote_lib_deletion", "delete"))
	require.NoError(t, unstructured.SetNestedField(u.Object, value, "__internal_use_espejote_lib_deletion", key))
	return u
}

func injectApplyOptions(t *testing.T, u *unstructured.Unstructured, key string, value any) *unstructured.Unstructured {
	t.Helper()

	require.NoError(t, unstructured.SetNestedField(u.Object, value, "__internal_use_espejote_lib_apply_options", key))
	return u
}
