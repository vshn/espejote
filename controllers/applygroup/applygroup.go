package applygroup

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"slices"

	"go.uber.org/multierr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	espejotev1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

var applyGroupConfigGVK = schema.GroupVersionKind{
	Group:   "internal.espejote.io",
	Version: "v1",
	Kind:    "ApplyGroupConfig",
}

//go:generate go tool golang.org/x/tools/cmd/stringer -type=Kind
type Kind int

const (
	NoopKind Kind = iota
	ApplyKind
	ApplyGroupConfigKind
	DeleteKind
	GroupKind
)

// ApplyDefaults holds the default options for applying resources.
type ApplyDefaults struct {
	espejotev1alpha1.ApplyOptions
	// FieldManagerFallback is the field manager to use if none is specified for the object.
	FieldManagerFallback string
}

// Applier represents a deeply nested structure of resources to apply or delete.
// Best used by unmarshalling from JSON.
//
//	applier := applygroup.Applier{
//		ApplyDefaults: applygroup.ApplyDefaults{FieldManagerFallback: "applier"},
//	}
//
//	err := json.Unmarshal([]byte(rendered), &applier)
type Applier struct {
	Kind Kind

	Parent        *Applier
	ApplyDefaults ApplyDefaults
	ErrorPolicy   string

	ResourceApplyOptions  []client.PatchOption
	ResourceDeleteOptions []client.DeleteOption
	Resource              *unstructured.Unstructured

	Group []Applier
}

// UnmarshalJSONFrom implements jsonv2.UnmarshalerFrom.
// It unmarshals a deeply nested structure of resources to apply or delete.
// The JSON can be:
// - null: represents a NoopKind
// - an object: represents a resource to apply or delete
// - an array: represents a group of resources to apply or delete
//
// If the object is of kind ApplyGroupConfig, it is treated as a configuration for the group.
// It must not be the root element.
// If the array contains an ApplyGroupConfig, it must be the first element in the array.
// The errorPolicy is inherited by child groups and defaults to "Continue".
//
// If the object has the special deletion options, it is treated as a resource to delete.
// Otherwise, it is treated as a resource to apply.
//
// Example JSON:
//
//	[
//	  null,
//	  [
//	    {
//	      "apiVersion": "internal.espejote.io/v1",
//	      "kind": "ApplyGroupConfig",
//	      "spec": {
//	        "errorPolicy": "Abort"
//	      }
//	    },
//	    {
//	      "apiVersion": "v1",
//	      "kind": "ConfigMap"
//	    },
//	    {
//	      "apiVersion": "v1",
//	      "kind": "ConfigMap"
//	    }
//	  ],
//	  {
//	    "apiVersion": "apps/v1",
//	    "kind": "Deployment"
//	  }
//	]
func (a *Applier) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	switch d.PeekKind() {
	case 'n':
		a.Kind = NoopKind
		return d.SkipValue()
	case '{':
		u := new(unstructured.Unstructured)
		if err := json.UnmarshalDecode(d, u); err != nil {
			return fmt.Errorf("decoding resource: %w", err)
		}
		a.Resource = u
		if u.GetObjectKind().GroupVersionKind() == applyGroupConfigGVK && a.Parent == nil {
			return fmt.Errorf("%s cannot be the root element", applyGroupConfigGVK.Kind)
		} else if u.GetObjectKind().GroupVersionKind() == applyGroupConfigGVK {
			a.Kind = ApplyGroupConfigKind
		} else {
			a.Kind = ApplyKind

			shouldDelete, delOpts, err := deleteOptionsFromRenderedObject(u)
			if err != nil {
				return fmt.Errorf("getting deletion options: %w", err)
			}
			if shouldDelete {
				a.Kind = DeleteKind
				a.ResourceDeleteOptions = delOpts
				a.Resource = stripUnstructuredForDelete(u)
			} else {
				po, err := patchOptionsFromObject(a.ApplyDefaults, u)
				if err != nil {
					return fmt.Errorf("getting patch options: %w", err)
				}
				a.ResourceApplyOptions = po
			}
		}

		return nil
	case '[':
		a.Kind = GroupKind

		if _, err := d.ReadToken(); err != nil {
			return fmt.Errorf("reading array start: %w", err)
		}

		for i := 0; d.PeekKind() != ']' && d.PeekKind() != 0; i++ {
			elem := Applier{ApplyDefaults: a.ApplyDefaults, Parent: a}
			if err := json.UnmarshalDecode(d, &elem); err != nil {
				return fmt.Errorf("decoding group element: %w", err)
			}
			if elem.Kind == ApplyGroupConfigKind && i != 0 {
				return fmt.Errorf("%s must be the first element in an array", applyGroupConfigGVK.Kind)
			} else if elem.Kind == ApplyGroupConfigKind {
				errorPolicy, found, err := unstructured.NestedString(elem.Resource.Object, "spec", "errorPolicy")
				if err != nil {
					return fmt.Errorf("reading errorPolicy: %w", err)
				}
				if found {
					if !slices.Contains([]string{"Continue", "Abort"}, errorPolicy) {
						return fmt.Errorf("errorPolicy must be either 'Continue' or 'Abort', got '%s'", errorPolicy)
					}
					a.ErrorPolicy = errorPolicy
				}
			} else {
				a.Group = append(a.Group, elem)
			}
		}
		if a.ErrorPolicy == "" && a.Parent != nil {
			a.ErrorPolicy = a.Parent.ErrorPolicy
		}
		if _, err := d.ReadToken(); err != nil {
			return fmt.Errorf("reading array end: %w", err)
		}

		return nil
	}
	return fmt.Errorf("unexpected JSON token kind: %s", d.PeekKind())
}

// Walk calls the given function for this Applier and all its children recursively in DFS order.
// If the function returns an error, the walk is aborted and the error is returned.
func (a *Applier) Walk(f func(*Applier) error) error {
	if err := f(a); err != nil {
		return err
	}
	for i := range a.Group {
		if err := a.Group[i].Walk(f); err != nil {
			return err
		}
	}
	return nil
}

// Apply applies or deletes the resource(s) represented by this Applier using the given client.
// It returns an error if any operation fails.
// For groups, it respects the errorPolicy: "Abort" stops on the first error, "Continue" collects all errors.
func (a *Applier) Apply(ctx context.Context, cli client.Writer) error {
	switch a.Kind {
	case NoopKind:
		return nil
	case ApplyKind:
		if a.Resource == nil {
			return fmt.Errorf("apply kind requires a resource")
		}
		if err := cli.Patch(ctx, a.Resource, client.Apply, a.ResourceApplyOptions...); err != nil {
			return fmt.Errorf("applying resource %s/%s %s: %w", a.Resource.GetNamespace(), a.Resource.GetName(), a.Resource.GetKind(), err)
		}
		return nil
	case DeleteKind:
		if a.Resource == nil {
			return fmt.Errorf("delete kind requires a resource")
		}
		if err := cli.Delete(ctx, stripUnstructuredForDelete(a.Resource), a.ResourceDeleteOptions...); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting resource %s/%s %s: %w", a.Resource.GetNamespace(), a.Resource.GetName(), a.Resource.GetKind(), err)
		}
		return nil
	case GroupKind:
		var errors []error
		for _, child := range a.Group {
			if err := child.Apply(ctx, cli); err != nil {
				switch a.ErrorPolicy {
				case "Abort":
					return err
				case "", "Continue":
					errors = append(errors, err)
				}
			}
		}
		return multierr.Combine(errors...)
	case ApplyGroupConfigKind:
		return fmt.Errorf("cannot apply ApplyGroupConfig directly")
	default:
		return fmt.Errorf("unknown kind: %d", a.Kind)
	}
}

// patchOptionsFromObject returns the patch options for the given ManagedResource and object.
// The options are merged from the ManagedResource's ApplyOptions and the object's annotations.
// Object annotations take precedence over ManagedResource's ApplyOptions.
// The options are:
// - FieldValidation: the field validation mode (default: "Strict")
// - FieldManager: the field manager/owner
// - ForceOwnership: if true, the ownership is forced (default: false)
// Warning: this function modifies the object by removing the options from the annotations.
func patchOptionsFromObject(defaults ApplyDefaults, obj *unstructured.Unstructured) ([]client.PatchOption, error) {
	const optionsKey = "__internal_use_espejote_lib_apply_options"

	fieldValidation := defaults.FieldValidation
	objFieldValidation, ok, err := unstructured.NestedString(obj.UnstructuredContent(), optionsKey, "fieldValidation")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option field validation: %w", err)
	}
	if ok {
		fieldValidation = objFieldValidation
	}
	if fieldValidation == "" {
		fieldValidation = "Strict"
	}

	fieldManager := defaults.FieldManager
	objFieldManager, ok, err := unstructured.NestedString(obj.UnstructuredContent(), optionsKey, "fieldManager")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option fieldManager: %w", err)
	}
	if ok {
		fieldManager = objFieldManager
	}
	if fieldManager == "" {
		fieldManager = defaults.FieldManagerFallback
	}
	objFieldManagerSuffix, _, err := unstructured.NestedString(obj.UnstructuredContent(), optionsKey, "fieldManagerSuffix")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option fieldManagerSuffix: %w", err)
	}
	fieldManager += objFieldManagerSuffix

	po := []client.PatchOption{client.FieldValidation(fieldValidation), client.FieldOwner(fieldManager)}

	objForce, ok, err := unstructured.NestedBool(obj.UnstructuredContent(), optionsKey, "force")
	if err != nil {
		return nil, fmt.Errorf("failed to get apply option force: %w", err)
	}
	if ok {
		if objForce {
			po = append(po, client.ForceOwnership)
		}
	} else if defaults.Force {
		po = append(po, client.ForceOwnership)
	}

	unstructured.RemoveNestedField(obj.UnstructuredContent(), optionsKey)

	return po, nil
}

// stripUnstructuredForDelete returns a copy of the given unstructured object with only the GroupVersionKind, Namespace and Name set.
func stripUnstructuredForDelete(u *unstructured.Unstructured) *unstructured.Unstructured {
	cp := &unstructured.Unstructured{}
	cp.SetGroupVersionKind(u.GroupVersionKind())
	cp.SetNamespace(u.GetNamespace())
	cp.SetName(u.GetName())
	return cp
}

// deleteOptionsFromRenderedObject extracts the deletion options from the given unstructured object.
// The deletion options are stored in the object under the "__internal_use_espejote_lib_deletion" key.
// The deletion options are:
// - delete: bool, required, if true the object should be deleted
// - gracePeriodSeconds: int, optional, the grace period for the deletion
// - propagationPolicy: string, optional, the deletion propagation policy
// - preconditionUID: string, optional, the UID of the object that must match for deletion
// - preconditionResourceVersion: string, optional, the resource version of the object that must match for deletion
// The first return value is true if the object should be deleted.
func deleteOptionsFromRenderedObject(obj *unstructured.Unstructured) (shouldDelete bool, opts []client.DeleteOption, err error) {
	const deletionKey = "__internal_use_espejote_lib_deletion"

	shouldDelete, _, err = unstructured.NestedBool(obj.UnstructuredContent(), deletionKey, "delete")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion flag: %w", err)
	}
	if !shouldDelete {
		return false, nil, nil
	}

	gracePeriodSeconds, ok, err := unstructured.NestedInt64(obj.UnstructuredContent(), deletionKey, "gracePeriodSeconds")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion grace period: %w", err)
	}
	if ok {
		opts = append(opts, client.GracePeriodSeconds(gracePeriodSeconds))
	}

	propagationPolicy, ok, err := unstructured.NestedString(obj.UnstructuredContent(), deletionKey, "propagationPolicy")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion propagation policy: %w", err)
	}
	if ok {
		opts = append(opts, client.PropagationPolicy(metav1.DeletionPropagation(propagationPolicy)))
	}

	preconditions := metav1.Preconditions{}
	hasPreconditions := false
	preconditionUID, ok, err := unstructured.NestedString(obj.UnstructuredContent(), deletionKey, "preconditionUID")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion precondition UID: %w", err)
	}
	if ok {
		hasPreconditions = true
		preconditions.UID = ptr.To(types.UID(preconditionUID))
	}
	preconditionResourceVersion, ok, err := unstructured.NestedString(obj.UnstructuredContent(), deletionKey, "preconditionResourceVersion")
	if err != nil {
		return false, nil, fmt.Errorf("failed to get deletion precondition resource version: %w", err)
	}
	if ok {
		hasPreconditions = true
		preconditions.ResourceVersion = &preconditionResourceVersion
	}
	if hasPreconditions {
		opts = append(opts, client.Preconditions(preconditions))
	}

	return
}
