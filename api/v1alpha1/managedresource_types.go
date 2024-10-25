/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// 👇 Finish the description
// ManagedResourceSpec defines the <INSERT_TEXT>.
type ManagedResourceSpec struct {
	// +kubebuilder:validation:Optional
	Context []Context `json:"context,omitempty"`

	// +kubebuilder:validation:Required
	// Template defines the Jsonnet snippet that generates the output.
	Template string `json:"template"`
}

// Context defines resources used in the template.
// Changes to these resources will trigger a reconcile the ManagedResource.
type Context struct {
	// +kubebuilder:validation:Required
	// Alias defines the name of the ext-var in the Jsonnet template.
	Alias string `json:"alias"`

	// +kubebuilder:validation:Required
	// APIVersion defines the versioned schema of this representation of an object.
	APIVersion string `json:"apiVersion"`

	// +kubebuilder:validation:Required
	// Kind is a string value representing the REST resource this object represents.
	Kind string `json:"kind"`

	// +kubebuilder:validation:Optional
	// Name defines the name of the resource to be targeted.
	// If left empty all resources of that kind will be targeted.
	Name string `json:"name,omitempty"`

	// +kubebuilder:validation:Optional
	// Namespace defines the namespaces of the resource to be targeted.
	// If left empty all valid namespaces will be targeted.
	Namespace string `json:"namespace,omitempty"`

	// +kubebuilder:validation:Optional
	// LabelSelector filters results by label.
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// ManagedResourceStatus defines the observed state of ManagedResource.
type ManagedResourceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ManagedResource is the Schema for the managedresources API.
type ManagedResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedResourceSpec   `json:"spec,omitempty"`
	Status ManagedResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ManagedResourceList contains a list of ManagedResource.
type ManagedResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedResource{}, &ManagedResourceList{})
}

func (mr *ManagedResource) GetParserName() string {
	return fmt.Sprintf("%s/%s", mr.Namespace, mr.Name)
}

func (mr *ManagedResource) GetContext() []Context {
	return mr.Spec.Context
}

func (mr *ManagedResource) GetTemplate() string {
	return mr.Spec.Template
}

func (mr *ManagedResource) GetNamespaceSelector() NamespaceSelector {
	return NamespaceSelector{
		MatchNames: []string{mr.Namespace},
	}
}

func (mr *ManagedResource) IsNamespaced() bool {
	return true
}
