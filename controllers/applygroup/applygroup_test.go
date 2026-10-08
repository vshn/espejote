package applygroup_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/vshn/espejote/controllers/applygroup"
)

func Test_UnmarshalJSON(t *testing.T) {
	abortGroupConf := mustMarshal(t, groupWithErrorPolicy("Abort"))
	continueGroupConf := mustMarshal(t, groupWithErrorPolicy("Continue"))

	for _, tc := range []struct {
		name     string
		defaults applygroup.ApplyDefaults
		json     string
		expected func(applygroup.ApplyDefaults) applygroup.Applier
		matchErr string
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
					ResourceApplyOptions: []applygroup.ApplyOption{
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
					ResourceApplyOptions: []applygroup.ApplyOption{
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
					ResourceApplyOptions: []applygroup.ApplyOption{
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
						ResourceApplyOptions: []applygroup.ApplyOption{
							client.FieldOwner("default-field-manager"),
						},
					},
					groupWithErrPolicyFromParent,
					secondInnerGroup,
				}
				return outerMost
			},
		},
		{
			name: "subresource",
			defaults: applygroup.ApplyDefaults{
				FieldManagerFallback: "default-field-manager",
			},
			json: mustMarshal(t, injectApplyOptions(t, mustUnmarshalUnstructured(t, `{"kind":"Example"}`), "subResource", "status")),
			expected: func(def applygroup.ApplyDefaults) applygroup.Applier {
				return applygroup.Applier{
					ApplyDefaults: def,
					Kind:          applygroup.ApplyKind,
					Resource:      mustUnmarshalUnstructured(t, `{"kind":"Example"}`),
					ResourceApplyOptions: []applygroup.ApplyOption{
						client.FieldOwner(def.FieldManagerFallback),
					},
					SubResource: "status",
				}
			},
		},
		{
			name:     "invalid - no kind",
			json:     `{"apiVersion":"v1","metadata":{"name":"test"}}`,
			matchErr: "'Kind' is missing",
		},
		{
			name:     "invalid - invalid delete options",
			json:     mustMarshal(t, injectDeleteOptions(t, mustUnmarshalUnstructured(t, `{"kind":"Example"}`), "gracePeriodSeconds", "abc")),
			matchErr: "accessor error",
		},
		{
			name:     "invalid - invalid apply options",
			json:     mustMarshal(t, injectApplyOptions(t, mustUnmarshalUnstructured(t, `{"kind":"Example"}`), "fieldManager", true)),
			matchErr: "accessor error",
		},
		{
			name:     "invalid - wrong type group config",
			json:     "[" + mustMarshal(t, groupWithErrorPolicy(false)) + "]",
			matchErr: "accessor error",
		},
		{
			name:     "invalid - wrong group config",
			json:     "[" + mustMarshal(t, groupWithErrorPolicy("KindaWrong")) + "]",
			matchErr: "errorPolicy must be either 'Continue' or 'Abort', got 'KindaWrong'",
		},
		{
			name:     "invalid - group config is root element",
			json:     continueGroupConf,
			matchErr: "ApplyGroupConfig cannot be the root element",
		},
		{
			name:     "invalid - group config is not first in group",
			json:     `[{"kind":"Example"},` + continueGroupConf + `]`,
			matchErr: "ApplyGroupConfig must be the first element in an array",
		},
		{
			name:     "invalid - array with non-object",
			json:     `[42]`,
			matchErr: "unexpected JSON token kind: number",
		},
		{
			name:     "invalid - array not closed",
			json:     `[`,
			matchErr: "unexpected EOF",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := applygroup.Applier{ApplyDefaults: tc.defaults}
			if tc.matchErr != "" {
				require.ErrorContains(t, json.Unmarshal([]byte(tc.json), &a), tc.matchErr)
				return
			}
			require.NoError(t, json.Unmarshal([]byte(tc.json), &a))
			require.Equal(t, tc.expected(tc.defaults), a)
		})
	}
}

func Test_Apply(t *testing.T) {
	for _, tc := range []struct {
		name               string
		subject            *applygroup.Applier
		expectedApplyCalls []string
		matchErr           string
	}{
		{
			name:    "empty",
			subject: &applygroup.Applier{},
		},
		{
			name: "noop",
			subject: &applygroup.Applier{
				Kind: applygroup.NoopKind,
			},
		},
		{
			name: "apply",
			subject: &applygroup.Applier{
				Kind:     applygroup.ApplyKind,
				Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
			},
			expectedApplyCalls: []string{"Apply/Example//"},
		},
		{
			name: "apply subresource",
			subject: &applygroup.Applier{
				Kind:        applygroup.ApplyKind,
				Resource:    &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
				SubResource: "status",
			},
			expectedApplyCalls: []string{"status/Apply/Example//"},
		},
		{
			name: "delete",
			subject: &applygroup.Applier{
				Kind:     applygroup.DeleteKind,
				Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
			},
			expectedApplyCalls: []string{"Delete/Example//"},
		},
		{
			name: "group",
			subject: &applygroup.Applier{
				Kind: applygroup.GroupKind,
				Group: []applygroup.Applier{
					{
						Kind:     applygroup.ApplyKind,
						Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
					},
					{
						Kind:     applygroup.DeleteKind,
						Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
					},
					{
						Kind: applygroup.NoopKind,
					},
				},
			},
			expectedApplyCalls: []string{
				"Apply/Example//",
				"Delete/Example//",
			},
		},
		{
			name: "applygroup config is not apply-able",
			subject: &applygroup.Applier{
				Kind: applygroup.ApplyGroupConfigKind,
			},
			matchErr: "cannot apply ApplyGroupConfig directly",
		},
		{
			name: "group with error - default Continue",
			subject: &applygroup.Applier{
				Kind: applygroup.GroupKind,
				Group: []applygroup.Applier{
					{
						Kind: applygroup.Kind(42),
					},
					{
						Kind:     applygroup.DeleteKind,
						Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
					},
				},
			},
			expectedApplyCalls: []string{
				"Delete/Example//",
			},
			matchErr: "42",
		},
		{
			name: "group with error - explicit Continue",
			subject: &applygroup.Applier{
				Kind:        applygroup.GroupKind,
				ErrorPolicy: "Continue",
				Group: []applygroup.Applier{
					{
						Kind: applygroup.Kind(42),
					},
					{
						Kind:     applygroup.DeleteKind,
						Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
					},
				},
			},
			expectedApplyCalls: []string{
				"Delete/Example//",
			},
			matchErr: "42",
		},
		{
			name: "group with error - explicit Abort",
			subject: &applygroup.Applier{
				Kind:        applygroup.GroupKind,
				ErrorPolicy: "Abort",
				Group: []applygroup.Applier{
					{
						Kind: applygroup.Kind(42),
					},
					{
						Kind:     applygroup.DeleteKind,
						Resource: &unstructured.Unstructured{Object: map[string]any{"kind": "Example"}},
					},
				},
			},
			matchErr: "42",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeClient := &fakeClient{}
			err := tc.subject.Apply(t.Context(), fakeClient)
			if tc.matchErr != "" {
				require.ErrorContains(t, err, tc.matchErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expectedApplyCalls, fakeClient.calls)
		})
	}
}

func Test_Walk(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		encountered := []*applygroup.Applier{}
		subject := &applygroup.Applier{
			Group: []applygroup.Applier{
				{
					Kind: applygroup.NoopKind,
				},
				{
					Kind: applygroup.GroupKind,
					Group: []applygroup.Applier{
						{
							Kind: applygroup.NoopKind,
						},
						{
							Kind: applygroup.NoopKind,
						},
					},
				},
				{
					Kind: applygroup.NoopKind,
				},
			},
		}

		require.NoError(t, subject.Walk(func(a *applygroup.Applier) error {
			encountered = append(encountered, a)
			return nil
		}))

		require.Equal(t, []*applygroup.Applier{
			subject,
			&subject.Group[0],
			&subject.Group[1],
			&subject.Group[1].Group[0],
			&subject.Group[1].Group[1],
			&subject.Group[2],
		}, encountered)
	})

	t.Run("error", func(t *testing.T) {
		encountered := []*applygroup.Applier{}
		subject := &applygroup.Applier{
			Group: []applygroup.Applier{
				{
					Kind: applygroup.NoopKind,
				},
				{
					Kind: applygroup.GroupKind,
				},
				{
					Kind: applygroup.NoopKind,
				},
			},
		}
		returnedError := errors.New("some error")
		err := subject.Walk(func(a *applygroup.Applier) error {
			encountered = append(encountered, a)
			if a.Kind == applygroup.GroupKind {
				return returnedError
			}
			return nil
		})
		require.Equal(t, returnedError, err)
		require.Equal(t, []*applygroup.Applier{
			subject,
			&subject.Group[0],
			&subject.Group[1],
		}, encountered)
	})
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

func groupWithErrorPolicy(policy any) map[string]any {
	return map[string]any{
		"apiVersion": "internal.espejote.io/v1",
		"kind":       "ApplyGroupConfig",
		"spec": map[string]any{
			"errorPolicy": policy,
		},
	}
}

type fakeClient struct {
	calls []string
}

func (c *fakeClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	c.recordCallWithObj("Apply", obj)
	return nil
}

func (c *fakeClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.recordCallWithObj("Create", obj)
	return nil
}

func (c *fakeClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	c.recordCallWithObj("Delete", obj)
	return nil
}

func (c *fakeClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.recordCallWithObj("Update", obj)
	return nil
}

func (c *fakeClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.recordCallWithObj("Patch", obj)
	return nil
}

func (c *fakeClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	c.recordCallWithObj("DeleteAllOf", obj)
	return nil
}

func (c *fakeClient) SubResource(subResource string) client.SubResourceClient {
	return &fakeSubResourceClient{func(call string, o any) {
		c.recordCallWithObj(fmt.Sprintf("%s/%s", subResource, call), o)
	}}
}

func (c *fakeClient) recordCallWithObj(call string, o any) {
	type namedObj interface {
		GetObjectKind() schema.ObjectKind
		GetNamespace() string
		GetName() string
	}

	switch obj := o.(type) {
	case namedObj:
		c.calls = append(c.calls, strings.Join([]string{call, obj.GetObjectKind().GroupVersionKind().Kind, obj.GetNamespace(), obj.GetName()}, "/"))
	default:
		c.calls = append(c.calls, call)
	}
}

type fakeSubResourceClient struct {
	recordCallWithObj func(call string, o any)
}

func (c *fakeSubResourceClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	c.recordCallWithObj("Apply", obj)
	return nil
}

func (c *fakeSubResourceClient) Create(ctx context.Context, obj, obj2 client.Object, opts ...client.SubResourceCreateOption) error {
	c.recordCallWithObj("Create", obj)
	return nil
}

func (c *fakeSubResourceClient) Get(ctx context.Context, obj, obj2 client.Object, opts ...client.SubResourceGetOption) error {
	c.recordCallWithObj("Get", obj)
	return nil
}

func (c *fakeSubResourceClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	c.recordCallWithObj("Update", obj)
	return nil
}

func (c *fakeSubResourceClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	c.recordCallWithObj("Patch", obj)
	return nil
}
