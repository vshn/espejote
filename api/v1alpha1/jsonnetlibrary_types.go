package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// JsonnetLibrarySpec defines the desired state of JsonnetLibrary.
type JsonnetLibrarySpec struct {
	// Data is a map of Jsonnet library files.
	// The key is the file name and the value is the file content.
	Data map[string]string `json:"data,omitempty"`
}

// +kubebuilder:object:root=true

// JsonnetLibrary is the Schema for the jsonnetlibraries API.
type JsonnetLibrary struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec JsonnetLibrarySpec `json:"spec,omitempty"`
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
