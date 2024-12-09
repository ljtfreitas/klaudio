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

// ResourceGroupSpec defines the desired state of ResourceGroup
type ResourceGroupSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Parameters *runtime.RawExtension  `json:"parameters,omitempty"`
	Refs       []ResourceGroupRef     `json:"refs,omitempty"`
	Resources  []ResourceGroupElement `json:"resources,omitempty"`
}

type ResourceGroupRefKind string

const (
	ResourceGroupRefConfigMap = ResourceGroupRefKind("ConfigMap")
)

type ResourceGroupRef struct {
	Name       string               `json:"name"`
	ApiVersion string               `json:"apiVersion"`
	Kind       ResourceGroupRefKind `json:"kind"`
	Namespace  string               `json:"namespace,omitempty"`
}

type ResourceGroupElement struct {
	Name        string                `json:"name"`
	ResourceRef string                `json:"resourceRef"`
	Properties  *runtime.RawExtension `json:"properties"`
}

type ResourceGroupDeploymentStatuses map[string]ResourceGroupDeploymentStatus

type ResourceGroupStatusDescription string

var (
	ResourceGroupDeploymentInProgress = ResourceGroupStatusDescription("DeploymentInProgress")
)

// ResourceGroupStatus defines the observed state of ResourceGroup
type ResourceGroupStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Deployments ResourceGroupDeploymentStatuses `json:"deployments"`
	Status      ResourceGroupStatusDescription  `json:"status"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ResourceGroup is the Schema for the resourcegroups API
type ResourceGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceGroupSpec   `json:"spec,omitempty"`
	Status ResourceGroupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceGroupList contains a list of ResourceGroup
type ResourceGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceGroup{}, &ResourceGroupList{})
}
