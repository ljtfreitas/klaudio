package provisioning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const PulumiProvisionerName = "pulumi"

type PulumiProvisioner struct {
	client        client.Client
	dynamicClient *dynamic.DynamicClient
	scheme        *runtime.Scheme
	log           logr.Logger
	properties    *pulumiProvisionerProperties
}

type pulumiProvisionerProperties struct {
	Git pulumiProvisionerGitProperties `json:"git"`
}

type pulumiProvisionerGitProperties struct {
	Repo              string  `json:"repo"`
	Branch            *string `json:"branch"`
	Dir               *string `json:"dir"`
	IntervalInSeconds *int    `json:"intervalInSeconds"`
}

func newPulumiProvisioner(c client.Client, d *dynamic.DynamicClient, scheme *runtime.Scheme, log logr.Logger, provisioner *resourcesv1alpha1.ResourceRefProvisioner) (Provisioner, error) {
	properties := &pulumiProvisionerProperties{}
	if err := json.Unmarshal(provisioner.Properties.Raw, properties); err != nil {
		return nil, err
	}

	pulumiProvisioner := &PulumiProvisioner{
		client:        c,
		dynamicClient: d,
		scheme:        scheme,
		log:           log,
		properties:    properties,
	}

	return pulumiProvisioner, nil
}

func (provisioner *PulumiProvisioner) Run(ctx context.Context, resource *resourcesv1alpha1.Resource) (*ProvisionedResourceStatus, error) {
	provisioner.log.Info(fmt.Sprintf("starting OpenTofu provisioner to resource %s/%s...", resource.Namespace, resource.Name))

	stack, err := provisioner.getOrNewStack(ctx, resource)
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("running Stack: %s", stack.GetName()))

	stackStatus, exists, err := unstructured.NestedMap(stack.Object, "status")
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("status from Stack object %s is: %q", stack.GetName(), stackStatus))

	provisionedResource := &ProvisionedResource{
		GroupVersionKind: stack.GroupVersionKind(),
		Name:             resource.Name,
	}

	if exists {
		if lastUpdate, exists := stackStatus["lastUpdate"].(map[string]any); exists {
			outputs := stackStatus["outputs"].(map[string]any)

			provisioner.log.Info(fmt.Sprintf("Stack last update: %q", lastUpdate))
			provisioner.log.Info(fmt.Sprintf("Stack outputs: %q", outputs))

			switch lastUpdate["state"] {

			case "succeeded":
				status := &ProvisionedResourceStatus{
					Resource: provisionedResource,
					State:    ProvisionedResourceSuccessState,
					Outputs:  outputs,
				}
				return status, nil

			case "failed":
				status := &ProvisionedResourceStatus{
					Resource: provisionedResource,
					State:    ProvisionedResourceFailedState,
					Outputs:  outputs,
				}
				return status, nil
			}
		}
	}

	status := &ProvisionedResourceStatus{
		Resource: provisionedResource,
		State:    ProvisionedResourceRunningState,
		Outputs:  make(map[string]any),
	}

	return status, nil
}

func (provisioner *PulumiProvisioner) getOrNewStack(ctx context.Context, resource *resourcesv1alpha1.Resource) (*unstructured.Unstructured, error) {
	stackConfig := make(map[string]any)
	if err := json.Unmarshal(resource.Spec.Properties.Raw, &stackConfig); err != nil {
		return nil, err
	}

	newSpec := func() map[string]any {
		return map[string]any{
			"envRefs": map[string]any{
				"PULUMI_CONFIG_PASSPHRASE": map[string]any{
					"type": "Literal",
					"literal": map[string]any{
						"value": "",
					},
				},
			},
			"gitAuth": map[string]any{
				"accessToken": map[string]any{
					"type": "Secret",
					"secret": map[string]any{
						"name":      "github-access-token",
						"namespace": "default",
						"key":       "accessToken",
					},
				},
			},
			"stack":                  fmt.Sprintf("%s.%s", resource.Spec.Placement, resource.Name),
			"projectRepo":            provisioner.properties.Git.Repo,
			"branch":                 provisioner.properties.Git.Branch,
			"repoDir":                provisioner.properties.Git.Dir,
			"resyncFrequencySeconds": ptr.To(provisioner.properties.Git.IntervalInSeconds),
			"config":                 stackConfig,
		}
	}

	stackGvk := schema.GroupVersionKind{
		Group:   "pulumi.com",
		Version: "v1",
		Kind:    "Stack",
	}

	stackGvWithResource := stackGvk.GroupVersion().WithResource("stacks")

	stack, err := provisioner.dynamicClient.
		Resource(stackGvWithResource).
		Namespace(resource.Namespace).
		Get(ctx, resource.Name, metav1.GetOptions{})

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		stack = &unstructured.Unstructured{}
		stack.SetGroupVersionKind(stackGvk)

		object := make(map[string]any)

		object["apiVersion"] = "pulumi.com/v1"
		object["kind"] = "Stack"
		object["metadata"] = map[string]any{
			"name":      resource.Name,
			"namespace": resource.Namespace,
		}
		object["spec"] = newSpec()

		stack.SetUnstructuredContent(object)

		resourceGkv, err := apiutil.GVKForObject(resource, provisioner.scheme)
		if err != nil {
			return nil, err
		}

		stack.SetLabels(map[string]string{
			"name":      resource.Name,
			"namespace": resource.Namespace,
			resourcesv1alpha1.Group + "/managedBy.group":   resourceGkv.Group,
			resourcesv1alpha1.Group + "/managedBy.version": resourceGkv.Version,
			resourcesv1alpha1.Group + "/managedBy.kind":    resourceGkv.Kind,
			resourcesv1alpha1.Group + "/managedBy.name":    resource.Name,
			resourcesv1alpha1.Group + "/placement":         resource.Spec.Placement,
		})
		stack.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         resourceGkv.GroupVersion().String(),
				Kind:               resourceGkv.Kind,
				Name:               resource.Name,
				UID:                resource.UID,
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			},
		})

		if err := provisioner.client.Create(ctx, stack); err != nil {
			return nil, err
		}
	} else {
		stack.Object["spec"] = newSpec()
	}

	return stack, nil
}
