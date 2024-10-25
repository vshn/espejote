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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NamespaceSelector provides a way to specify targeted namespaces
type NamespaceSelector struct {
	// LabelSelector of namespaces to be targeted.
	// Can be combined with MatchNames to include unlabelled namespaces.
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// MatchNames lists namespace names to be targeted.
	// Each entry can be a Regex pattern.
	// A namespace is included if at least one pattern matches.
	// Invalid patterns will cause the sync to be cancelled and the status conditions will contain the error message.
	MatchNames []string `json:"matchNames,omitempty"`

	// IgnoreNames lists namespace names to be ignored.
	// Each entry can be a Regex pattern and if they match
	// the namespaces will be excluded from the sync even if matching in "matchNames" or via LabelSelector.
	// A namespace is ignored if at least one pattern matches.
	// Invalid patterns will cause the sync to be cancelled and the status conditions will contain the error message.
	IgnoreNames []string `json:"ignoreNames,omitempty"`
}

// ClusterManagedResourceSpec defines the desired state of ClusterManagedResource.
type ClusterManagedResourceSpec struct {
	// +kubebuilder:validation:Optional
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`

	// +kubebuilder:validation:Optional
	Context []Context `json:"context,omitempty"`

	// +kubebuilder:validation:Required
	// Template defines the Jsonnet snippet that generates the output.
	Template string `json:"template"`
}

// ClusterManagedResourceStatus defines the observed state of ClusterManagedResource.
type ClusterManagedResourceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ClusterManagedResource is the Schema for the clustermanagedresources API.
type ClusterManagedResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterManagedResourceSpec   `json:"spec,omitempty"`
	Status ClusterManagedResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterManagedResourceList contains a list of ClusterManagedResource.
type ClusterManagedResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterManagedResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterManagedResource{}, &ClusterManagedResourceList{})
}
