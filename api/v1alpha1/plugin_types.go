package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PluginSpec defines the desired state of Plugin.
type PluginSpec struct {
	// Module is the location of the WASM module to be loaded.
	// Can be an artifact served by an OCI-compatible registry (registry://).
	// If prefix is missing, it will default to registry://.
	//+required
	Module string `json:"module,omitempty"`
}

// PluginStatus defines the observed state of Plugin.
type PluginStatus struct {
	// Status reports the last overall status of the plugin.
	Status string `json:"status,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Plugin is the Schema for the Plugins API.
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`
type Plugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PluginSpec   `json:"spec,omitempty"`
	Status PluginStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PluginList contains a list of Plugin
type PluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Plugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Plugin{}, &PluginList{})
}
