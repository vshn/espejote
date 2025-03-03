package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterManagedResourceSpec defines the desired state of ClusterManagedResource
type ClusterManagedResourceSpec struct {
}

// ClusterManagedResourceStatus defines the observed state of ClusterManagedResource
type ClusterManagedResourceStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// ClusterManagedResource is the Schema for the ClusterManagedResources API
type ClusterManagedResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterManagedResourceSpec   `json:"spec,omitempty"`
	Status ClusterManagedResourceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClusterManagedResourceList contains a list of ClusterManagedResource
type ClusterManagedResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterManagedResource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterManagedResource{}, &ClusterManagedResourceList{})
}
