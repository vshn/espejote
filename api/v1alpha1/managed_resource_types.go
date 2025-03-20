package v1alpha1

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedResourceSpec defines the desired state of ManagedResource
type ManagedResourceSpec struct {
	// Triggers define the resources that trigger the reconciliation of the ManagedResource
	// Trigger information will be injected when rendering the template.
	// This can be used to only partially render the template based on the trigger.
	// +optional
	Triggers []ManagedResourceTrigger `json:"triggers,omitempty"`
	// Context defines the context for the ManagedResource
	Context []ManagedResourceContext `json:"context,omitempty"`

	// ServiceAccountRef is the service account this managed resource runs as.
	// The service account must have the necessary permissions to manage the resources referenced in the template.
	// If not set, the namespace's default service account is used.
	// +kubebuilder:default={"name": "default"}
	ServiceAccountRef corev1.LocalObjectReference `json:"serviceAccountRef,omitempty"`

	// Template defines the template for the ManagedResource
	// The template is rendered using Jsonnet and the result is applied to the cluster.
	// The template can reference the context and trigger information.
	// All access to injected data should be done through the `espejote.libsonnet` import.
	// The template can reference JsonnetLibrary objects by importing them.
	// JsonnetLibrary objects have the following structure:
	// - "espejote.libsonnet": The built in library for accessing the context and trigger information.
	// - "lib/<NAME>/<KEY>" libraries in the shared library namespace. The name corresponds to the name of the JsonnetLibrary object and the key to the key in the data field.
	//   The namespace is configured at controller startup and normally points to the namespace of the controller.
	// - "<NAME>/<KEY>" libraries in the same namespace as the ManagedResource. The name corresponds to the name of the JsonnetLibrary object and the key to the key in the data field.
	// The template can return a single object, a list of objects, or null. Everything else is considered an error.
	// Namespaced objects default to the namespace of the ManagedResource.
	Template string `json:"template,omitempty"`

	// ApplyOptions defines the options for applying the ManagedResource
	ApplyOptions ApplyOptions `json:"applyOptions,omitempty"`
}

type ApplyOptions struct {
	// FieldManager is the field manager to use when applying the ManagedResource
	// If not set, the field manager is set to the name of the resource with `managed-resource` prefix
	// +optional
	FieldManager string `json:"fieldManager,omitempty"`

	// Force is going to "force" Apply requests. It means user will
	// re-acquire conflicting fields owned by other people.
	// +optional
	// +kubebuilder:default=false
	Force bool `json:"force,omitempty"`

	// fieldValidation instructs the managed resource on how to handle
	// objects containing unknown or duplicate fields. Valid values are:
	// - Ignore: This will ignore any unknown fields that are silently
	// dropped from the object, and will ignore all but the last duplicate
	// field that the decoder encounters.
	// Note that Jsonnet won't allow you to add duplicate fields to an object
	// and most unregistered fields will error out in the server-side apply
	// request, even with this option set.
	// - Strict: This will fail the request with a BadRequest error if
	// any unknown fields would be dropped from the object, or if any
	// duplicate fields are present. The error returned will contain
	// all unknown and duplicate fields encountered.
	// Defaults to "Strict".
	// +kubebuilder:validation:Enum=Ignore;Strict
	// +kubebuilder:default=Strict
	// +optional
	FieldValidation string `json:"fieldValidation,omitempty"`
}

type ManagedResourceTrigger struct {
	// Name is the name of the trigger. The trigger can be referenced in the template by this name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Interval defines the interval at which the ManagedResource should be reconciled.
	// +kubebuilder:validation:Format=duration
	Interval metav1.Duration `json:"interval,omitempty"`

	// WatchResource defines one or multiple resources that trigger the reconciliation of the ManagedResource.
	// Resource information is injected when rendering the template and can be retrieved using `(import "espejote.libsonnet").getTrigger()`.
	// `local esp = import "espejote.libsonnet"; esp.triggerType() == esp.TriggerTypeWatchResource` will be true if the render was triggered by a definition in this block.
	// +optional
	WatchResource TriggerWatchResource `json:"watchResource,omitempty"`
}

// +kubebuilder:object:generate=false
type ClusterResource interface {
	fmt.Stringer

	GetVersion() string
	GetGroup() string
	GetKind() string

	GetName() string
	GetNamespace() *string

	GetLabelSelector() *metav1.LabelSelector
	GetMatchNames() []string
	GetIgnoreNames() []string
}

var _ ClusterResource = TriggerWatchResource{}

type TriggerWatchResource struct {
	// APIVersion of the resource that should be watched.
	// The APIVersion can be in the form "group/version" or "version".
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind of the resource that should be watched.
	Kind string `json:"kind,omitempty"`

	// Name of the resource that should be watched.
	// If not set, all resources of the specified Kind are watched.
	Name string `json:"name,omitempty"`
	// Namespace for the resources that should be watched.
	// If not set, the namespace of the ManagedResource is used.
	// Can be explicitly set to empty string to watch all namespaces.
	Namespace *string `json:"namespace,omitempty"`

	// LabelSelector can be used to filter the resources that should be watched.
	// This is efficiently done by the Kubernetes API server
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// MatchNames can be used to filter the resources that should be watched.
	// This is considered experimental and might be removed in the future.
	// The filtering is done on the controller side and might not be as efficient as the LabelSelector.
	// Filtered objects are dropped before any caching or processing.
	MatchNames []string `json:"matchNames,omitempty"`
	// IgnoreNames can be used to filter the resources that should be watched.
	// This is considered experimental and might be removed in the future.
	// The filtering is done on the controller side and might not be as efficient as the LabelSelector.
	// Filtered objects are dropped before any caching or processing.
	IgnoreNames []string `json:"ignoreNames,omitempty"`
}

func (t TriggerWatchResource) GetVersion() string {
	if p := strings.SplitN(t.APIVersion, "/", 2); len(p) == 2 {
		return p[1]
	}
	return t.APIVersion
}

func (t TriggerWatchResource) GetGroup() string {
	if p := strings.SplitN(t.APIVersion, "/", 2); len(p) == 2 {
		return p[0]
	}
	return ""
}

func (t TriggerWatchResource) GetKind() string {
	return t.Kind
}

func (t TriggerWatchResource) GetName() string {
	return t.Name
}

func (t TriggerWatchResource) GetNamespace() *string {
	return t.Namespace
}

func (t TriggerWatchResource) GetLabelSelector() *metav1.LabelSelector {
	return t.LabelSelector
}

func (t TriggerWatchResource) GetMatchNames() []string {
	return t.MatchNames
}

func (t TriggerWatchResource) GetIgnoreNames() []string {
	return t.IgnoreNames
}

func (t TriggerWatchResource) String() string {
	gvk := metav1.GroupVersionKind{
		Group:   t.GetGroup(),
		Version: t.GetVersion(),
		Kind:    t.Kind,
	}
	ns := "empty"
	if t.Namespace != nil {
		ns = fmt.Sprintf("%q", *t.Namespace)
	}
	return fmt.Sprintf("type=%q,name=%q,namespace=%s", gvk, t.Name, ns)
}

type ManagedResourceContext struct {
	// Name is the name of the context definition. The context can be referenced in the template by this name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Resource defines the resource that should be added to the context.
	// Adds a list of zero or more resources to the context.
	Resource ContextResource `json:"resource,omitempty"`
}

var _ ClusterResource = ContextResource{}

type ContextResource struct {
	// APIVersion of the resource that should be added to the context.
	APIVersion string `json:"apiVersion,omitempty"`
	// Group of the resource that should be added to the context.
	Group string `json:"group,omitempty"`
	// Kind of the resource that should be added to the context.
	Kind string `json:"kind,omitempty"`

	// Name of the resource that should be added to the context.
	// If not set, all resources of the specified Kind are added to the context.
	Name string `json:"name,omitempty"`
	// Namespace for the resources that should be added to the context.
	// If not set, the namespace of the ManagedResource is used.
	// Can be set to empty string to add all namespaces.
	Namespace *string `json:"namespace,omitempty"`

	// LabelSelector can be used to filter the resources that should be added to the context.
	// This is efficiently done by the Kubernetes API server
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// MatchNames can be used to filter the resources that should be added to the context.
	// This is considered experimental and might be removed in the future.
	// The filtering is done on the controller side and might not be as efficient as the LabelSelector.
	// Filtered objects are dropped before any caching or processing.
	MatchNames []string `json:"matchNames,omitempty"`
	// IgnoreNames can be used to filter the resources that should be added to the context.
	// This is considered experimental and might be removed in the future.
	// The filtering is done on the controller side and might not be as efficient as the LabelSelector.
	// Filtered objects are dropped before any caching or processing.
	IgnoreNames []string `json:"ignoreNames,omitempty"`
}

func (t ContextResource) GetVersion() string {
	return t.APIVersion
}

func (t ContextResource) GetGroup() string {
	return t.Group
}

func (t ContextResource) GetKind() string {
	return t.Kind
}

func (t ContextResource) GetName() string {
	return t.Name
}

func (t ContextResource) GetNamespace() *string {
	return t.Namespace
}

func (t ContextResource) GetLabelSelector() *metav1.LabelSelector {
	return t.LabelSelector
}

func (t ContextResource) GetMatchNames() []string {
	return t.MatchNames
}

func (t ContextResource) GetIgnoreNames() []string {
	return t.IgnoreNames
}

func (t ContextResource) String() string {
	gvk := metav1.GroupVersionKind{
		Group:   t.Group,
		Version: t.APIVersion,
		Kind:    t.Kind,
	}
	ns := "empty"
	if t.Namespace != nil {
		ns = fmt.Sprintf("%q", *t.Namespace)
	}
	return fmt.Sprintf("type=%q,name=%q,namespace=%s", gvk, t.Name, ns)
}

// ManagedResourceStatus defines the observed state of ManagedResource
type ManagedResourceStatus struct {
	// Status reports the last overall status of the ManagedResource
	// More information can be found by inspecting the ManagedResource's events with either `kubectl describe` or `kubectl get events`.
	Status string `json:"status,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ManagedResource is the Schema for the ManagedResources API
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`
type ManagedResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedResourceSpec   `json:"spec,omitempty"`
	Status ManagedResourceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ManagedResourceList contains a list of ManagedResource
type ManagedResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedResource{}, &ManagedResourceList{})
}
