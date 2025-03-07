package v1alpha1

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedResourceSpec defines the desired state of ManagedResource
type ManagedResourceSpec struct {
	// Triggers define the resources that trigger the reconciliation of the ManagedResource
	Triggers []ManagedResourceTrigger `json:"triggers,omitempty"`
	// Context defines the context for the ManagedResource
	Context []ManagedResourceContext `json:"context,omitempty"`

	// ServiceAccountRef is the service account to be used to run the controllers associated with this configuration
	// The default is the namespace's default service account
	ServiceAccountRef corev1.LocalObjectReference `json:"serviceAccountRef,omitempty"`

	// Template defines the template for the ManagedResource
	Template string `json:"template,omitempty"`
}

type ManagedResourceTrigger struct {
	// WatchResource defines one or multiple resources that trigger the reconciliation of the ManagedResource
	WatchResource TriggerWatchResource `json:"watchResource,omitempty"`
}

// +kubebuilder:object:generate=false
type ClusterResource interface {
	fmt.Stringer

	GetAPIVersion() string
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
	APIVersion string `json:"apiVersion,omitempty"`
	// Group of the resource that should be watched.
	Group string `json:"group,omitempty"`
	// Kind of the resource that should be watched.
	Kind string `json:"kind,omitempty"`

	// Name of the resource that should be watched.
	// If not set, all resources of the specified Kind are watched.
	Name string `json:"name,omitempty"`
	// Namespace for the resources that should be watched.
	// If not set, the namespace of the ManagedResource is used.
	// Can be set to empty string to watch all namespaces.
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

func (t TriggerWatchResource) GetAPIVersion() string {
	return t.APIVersion
}

func (t TriggerWatchResource) GetGroup() string {
	return t.Group
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

type ManagedResourceContext struct {
	// Def is the name of the context definition. The context can be referenced in the template by this name.
	Def string `json:"def"`

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

func (t ContextResource) GetAPIVersion() string {
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
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ManagedResource is the Schema for the ManagedResources API
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
