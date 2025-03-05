package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// JsonnetLibrarySpec defines the desired state of JsonnetLibrary.
type JsonnetLibrarySpec struct {
	Data map[string]string `json:"data,omitempty"`
}

// JsonnetLibraryStatus defines the observed state of JsonnetLibrary.
type JsonnetLibraryStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// JsonnetLibrary is the Schema for the jsonnetlibraries API.
type JsonnetLibrary struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   JsonnetLibrarySpec   `json:"spec,omitempty"`
	Status JsonnetLibraryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// JsonnetLibraryList contains a list of JsonnetLibrary.
type JsonnetLibraryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JsonnetLibrary `json:"items"`
}

func init() {
	SchemeBuilder.Register(&JsonnetLibrary{}, &JsonnetLibraryList{})
}
