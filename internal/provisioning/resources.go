package provisioning

import "k8s.io/apimachinery/pkg/runtime/schema"

type ProvisionedResourceStateDescription string

const (
	ProvisionedResourceRunningState = ProvisionedResourceStateDescription("Running")
	ProvisionedResourceFailedState  = ProvisionedResourceStateDescription("Failed")
	ProvisionedResourceSuccessState = ProvisionedResourceStateDescription("Success")
)

type ProvisionedResourceStatus struct {
	Resource *ProvisionedResource
	State    ProvisionedResourceStateDescription
	Outputs  map[string]any
}

type ProvisionedResource struct {
	schema.GroupVersionKind
	Name string
}

func (p *ProvisionedResourceStatus) IsRunning() bool {
	return p.State == ProvisionedResourceRunningState
}
