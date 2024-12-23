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
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ResourceRefSpec defines the desired state of ResourceRef
type ResourceRefSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Name        string                 `json:"name"`
	Kind        string                 `json:"kind"`
	Provisioner ResourceRefProvisioner `json:"provisioner"`
	Schema      ResourceRefSchema      `json:"schema"`
}

type ResourceRefProvisionerName string

const (
	ResourceRefPulumiProvisioner = "pulumi"
)

type ResourceRefProvisioner struct {
	Name       ResourceRefProvisionerName `json:"name"`
	Properties *runtime.RawExtension      `json:"properties,omitempty"`
}

type ResourceRefSchema struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`

	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Properties map[string]ResourceRefSchema `json:"properties,omitempty"`
}

type ResourceRefStatusDescription string

const (
	ResourceRefStatusDescriptionReady = "Ready"
)

// ResourceRefStatus defines the observed state of ResourceRef
type ResourceRefStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Status     ResourceRefStatusDescription `json:"status"`
	Placements []string                     `json:"placements"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.status`

// ResourceRef is the Schema for the resourcerefs API
type ResourceRef struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceRefSpec   `json:"spec,omitempty"`
	Status ResourceRefStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceRefList contains a list of ResourceRef
type ResourceRefList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceRef `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceRef{}, &ResourceRefList{})
}
