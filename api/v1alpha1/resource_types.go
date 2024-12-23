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

// ResourceSpec defines the desired state of Resource
type ResourceSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Placement   string                `json:"placement"`
	ResourceRef string                `json:"resourceRef"`
	Properties  *runtime.RawExtension `json:"properties"`
}

type ResourceStatusDescription string

const (
	ResourceDeployingStatusPhase = ResourceStatusDescription("Deploying")
	ResourceFailedStatusPhase    = ResourceStatusDescription("Failed")
	ResourceDoneStatusPhase      = ResourceStatusDescription("Done")

	ResourceConditionReady = "Ready"

	ResourceConditionReasonReconciling          = "Reconciling"
	ResourceConditionReasonDeploymentInProgress = "DeploymentInProgress"
	ResourceConditionReasonDeploymentDone       = "DeploymentDone"
	ResourceConditionReasonDeploymentFailed     = "DeploymentFailed"
)

type ResourceStatusProvisioner struct {
	Resource ResourceStatusProvisionerResource `json:"resource,omitempty"`
	State    string                            `json:"state,omitempty"`
}

type ResourceStatusProvisionerResource struct {
	Group   string `json:"group,omitempty"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name,omitempty"`
}

// ResourceStatus defines the observed state of Resource
type ResourceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Provisioner ResourceStatusProvisioner `json:"provisioner,omitempty"`
	Outputs     *runtime.RawExtension     `json:"outputs,omitempty"`
	Phase       ResourceStatusDescription `json:"phase,omitempty"`
	Conditions  []metav1.Condition        `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// Resource is the Schema for the resources API
type Resource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceSpec   `json:"spec,omitempty"`
	Status ResourceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceList contains a list of Resource
type ResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Resource `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Resource{}, &ResourceList{})
}
