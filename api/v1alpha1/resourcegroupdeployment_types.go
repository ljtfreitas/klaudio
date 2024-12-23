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

// ResourceGroupDeploymentSpec defines the desired state of ResourceGroupDeployment
type ResourceGroupDeploymentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Placement  string                 `json:"placement"`
	Refs       []ResourceGroupRef     `json:"refs,omitempty"`
	Parameters *runtime.RawExtension  `json:"parameters,omitempty"`
	Resources  []ResourceGroupElement `json:"resources,omitempty"`
}

type ResourceGroupDeploymentResourcesStatuses map[string]ResourceStatus

type ResourceGroupDeploymentStatusPhaseDescription string

const (
	DeploymentRunningPhase = ResourceGroupDeploymentStatusPhaseDescription("Running")
	DeploymentDonePhase    = ResourceGroupDeploymentStatusPhaseDescription("Done")

	DeploymentReadyCondition                        = "Ready"
	DeploymentConditionReasonReconciling            = "Reconciling"
	DeploymentConditionReasonResourceCreationFailed = "ResourceCreationFailed"
	DeploymentConditionReasonDeploymentInProgress   = "DeploymentInProgress"
	DeploymentConditionReasonDeploymentDone         = "DeploymentDone"
)

// ResourceGroupDeploymentStatus defines the observed state of ResourceGroupDeployment
type ResourceGroupDeploymentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Resources  ResourceGroupDeploymentResourcesStatuses      `json:"resources,omitempty"`
	Phase      ResourceGroupDeploymentStatusPhaseDescription `json:"phase,omitempty"`
	Conditions []metav1.Condition                            `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ResourceGroupDeployment is the Schema for the resourcegroupdeployments API
type ResourceGroupDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceGroupDeploymentSpec   `json:"spec,omitempty"`
	Status ResourceGroupDeploymentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceGroupDeploymentList contains a list of ResourceGroupDeployment
type ResourceGroupDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceGroupDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceGroupDeployment{}, &ResourceGroupDeploymentList{})
}
