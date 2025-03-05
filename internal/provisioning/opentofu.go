package provisioning

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const OpenTofuProvisionerName = "opentofu"

type OpenTofuProvisioner struct {
	client        client.Client
	dynamicClient *dynamic.DynamicClient
	scheme        *runtime.Scheme
	log           logr.Logger
	properties    *openTofuProvisionerProperties
}

type openTofuProvisionerProperties struct {
	Git openTofuProvisionerGitProperties `json:"git"`
}

type openTofuProvisionerGitProperties struct {
	Repo   string  `json:"repo"`
	Branch *string `json:"branch"`
	Dir    *string `json:"dir"`
}

func newOpenTofuProvisioner(c client.Client, d *dynamic.DynamicClient, scheme *runtime.Scheme, log logr.Logger, provisioner *resourcesv1alpha1.ResourceRefProvisioner) (Provisioner, error) {
	properties := &openTofuProvisionerProperties{}
	if err := json.Unmarshal(provisioner.Properties.Raw, properties); err != nil {
		return nil, err
	}

	openTofuProvisioner := &OpenTofuProvisioner{
		client:        c,
		dynamicClient: d,
		scheme:        scheme,
		log:           log,
		properties:    properties,
	}

	return openTofuProvisioner, nil
}

func (provisioner *OpenTofuProvisioner) Run(ctx context.Context, resource *resourcesv1alpha1.Resource) (*ProvisionedResourceStatus, error) {
	provisioner.log.Info(fmt.Sprintf("starting OpenTofu provisioner to resource %s/%s...", resource.Namespace, resource.Name))

	repo, err := provisioner.getOrNewRepo(ctx, resource)
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("using GitRepository: %s", repo.GetName()))

	terraform, err := provisioner.getOrNewTerraform(ctx, repo.GetName(), resource)
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("running Terraform: %s", terraform.GetName()))

	terraformStatus, err := status.Compute(terraform)
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("status from Terraform object %s is: %+v", terraform.GetName(), terraformStatus))

	provisionedResource := &ProvisionedResource{
		GroupVersionKind: terraform.GroupVersionKind(),
		Name:             resource.Name,
	}

	switch terraformStatus.Status {

	case status.InProgressStatus:
		status := &ProvisionedResourceStatus{
			Resource: provisionedResource,
			State:    ProvisionedResourceRunningState,
			Outputs:  make(map[string]any),
		}
		return status, nil

	case status.FailedStatus:
		status := &ProvisionedResourceStatus{
			Resource: provisionedResource,
			State:    ProvisionedResourceFailedState,
			Outputs:  make(map[string]any),
		}
		return status, nil
	}

	conditions, exists, err := unstructured.NestedSlice(terraform.Object, "status", "conditions")
	if err != nil {
		return nil, err
	}

	if exists {
		for _, condition := range conditions {
			conditionAsMap := condition.(map[string]any)

			conditionType := conditionAsMap["type"].(string)
			conditionStatus := conditionAsMap["status"].(string)
			if conditionType == "Ready" && conditionStatus == string(corev1.ConditionTrue) {
				outputs, err := provisioner.readTerraformOutputs(ctx, terraform)
				if err != nil {
					return nil, err
				}

				status := &ProvisionedResourceStatus{
					Resource: provisionedResource,
					State:    ProvisionedResourceSuccessState,
					Outputs:  outputs,
				}
				return status, nil
			}
		}
	}

	provisioner.log.Info(fmt.Sprintf("can't determine the Terraform provisioning status for object %s yet; keep running...", terraform.GetName()))

	resourceStatus := &ProvisionedResourceStatus{
		Resource: provisionedResource,
		State:    ProvisionedResourceRunningState,
		Outputs:  make(map[string]any),
	}

	return resourceStatus, nil

}

func (provisioner *OpenTofuProvisioner) getOrNewRepo(ctx context.Context, resource *resourcesv1alpha1.Resource) (*unstructured.Unstructured, error) {
	repoGvk := schema.GroupVersionKind{
		Group:   "source.toolkit.fluxcd.io",
		Version: "v1",
		Kind:    "GitRepository",
	}

	repoGvWithResource := repoGvk.GroupVersion().WithResource("gitrepo")

	repo, err := provisioner.dynamicClient.
		Resource(repoGvWithResource).
		Namespace(resource.Namespace).
		Get(ctx, resource.Spec.ResourceRef, metav1.GetOptions{})

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		repo = &unstructured.Unstructured{}
		repo.SetGroupVersionKind(repoGvk)

		content := make(map[string]any)
		content["apiVersion"] = "source.toolkit.fluxcd.io/v1"
		content["kind"] = "GitRepository"
		content["metadata"] = map[string]any{
			"name":      resource.Spec.ResourceRef,
			"namespace": resource.Namespace,
		}
		content["spec"] = map[string]any{
			"interval": "60s",
			"url":      provisioner.properties.Git.Repo,
			"ref": map[string]any{
				"branch": provisioner.properties.Git.Branch,
			},
		}

		repo.SetUnstructuredContent(content)

		resourceRef := &resourcesv1alpha1.ResourceRef{}
		if err := provisioner.client.Get(ctx, types.NamespacedName{Name: resource.Spec.ResourceRef}, resourceRef); err != nil {
			provisioner.log.Error(err, fmt.Sprintf("unable to fetch ResourceRef %s", resource.Spec.ResourceRef))
			return nil, err
		}

		resourceRefGvk := resourceRef.GroupVersionKind()

		repo.SetLabels(map[string]string{
			"name":      resource.Name,
			"namespace": resource.Namespace,
			resourcesv1alpha1.Group + "/managedBy.group":   resourceRefGvk.Group,
			resourcesv1alpha1.Group + "/managedBy.version": resourceRefGvk.Version,
			resourcesv1alpha1.Group + "/managedBy.kind":    resourceRefGvk.Kind,
			resourcesv1alpha1.Group + "/managedBy.name":    resourceRef.Name,
		})
		repo.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         resourceRefGvk.GroupVersion().String(),
				Kind:               resourceRefGvk.Kind,
				Name:               resourceRef.Name,
				UID:                resourceRef.UID,
				Controller:         ptr.To(true),
				BlockOwnerDeletion: ptr.To(true),
			},
		})

		if err := provisioner.client.Create(ctx, repo); err != nil {
			return nil, err
		}
	}

	return repo, nil
}

func (provisioner *OpenTofuProvisioner) getOrNewTerraform(ctx context.Context, gitRepoRef string, resource *resourcesv1alpha1.Resource) (*unstructured.Unstructured, error) {
	terraformGvk := schema.GroupVersionKind{
		Group:   "infra.contrib.fluxcd.io",
		Version: "v1alpha2",
		Kind:    "GitRepository",
	}

	terraformGvWithResource := terraformGvk.GroupVersion().WithResource("terraform")

	terraform, err := provisioner.dynamicClient.
		Resource(terraformGvWithResource).
		Namespace(resource.Namespace).
		Get(ctx, resource.Name, metav1.GetOptions{})

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		terraform = &unstructured.Unstructured{}
		terraform.SetGroupVersionKind(terraformGvk)

		inputs := make(map[string]any)
		if err := json.Unmarshal(resource.Spec.Properties.Raw, &inputs); err != nil {
			return nil, err
		}

		terraformVars := make([]map[string]any, 0, len(inputs))
		for name, input := range inputs {
			terraformVars = append(terraformVars, map[string]any{
				"name":  name,
				"value": input,
			})
		}

		object := make(map[string]any)

		object["apiVersion"] = "infra.contrib.fluxcd.io/v1alpha2"
		object["kind"] = "Terraform"
		object["metadata"] = map[string]any{
			"name":      resource.Name,
			"namespace": resource.Namespace,
		}
		object["spec"] = map[string]any{
			"interval":    "60s",
			"approvePlan": "auto",
			"path":        provisioner.properties.Git.Dir,
			"sourceRef": map[string]any{
				"kind":      "GitRepository",
				"name":      gitRepoRef,
				"namespace": resource.Namespace,
			},
			"vars": terraformVars,
			"writeOutputsToSecret": map[string]any{
				"name": fmt.Sprintf("%s-outputs", resource.Name),
			},
		}

		terraform.SetUnstructuredContent(object)

		resourceGkv, err := apiutil.GVKForObject(resource, provisioner.scheme)
		if err != nil {
			return nil, err
		}
		terraform.SetLabels(map[string]string{
			"name":      resource.Name,
			"namespace": resource.Namespace,
			resourcesv1alpha1.Group + "/managedBy.group":     resourceGkv.Group,
			resourcesv1alpha1.Group + "/managedBy.version":   resourceGkv.Version,
			resourcesv1alpha1.Group + "/managedBy.kind":      resourceGkv.Kind,
			resourcesv1alpha1.Group + "/managedBy.name":      resource.Name,
			resourcesv1alpha1.Group + "/managedBy.placement": resource.Spec.Placement,
		})
		terraform.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         resourceGkv.GroupVersion().String(),
				Kind:               resourceGkv.Kind,
				Name:               resource.Name,
				UID:                resource.UID,
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			},
		})

		if err := provisioner.client.Create(ctx, terraform); err != nil {
			return nil, err
		}
	}

	return terraform, nil
}

func (provisioner *OpenTofuProvisioner) readTerraformOutputs(ctx context.Context, terraform *unstructured.Unstructured) (map[string]any, error) {
	outputsSecretName, exists, err := unstructured.NestedString(terraform.Object, "spec", "writeOutputsToSecret", "name")
	if !exists {
		return nil, fmt.Errorf("impossible to read outputs; there are no secret defined in spec.writeOutputsToSecret in Terraform object %s", terraform.GetName())
	}

	provisioner.log.Info(fmt.Sprintf("trying to read outputs of Terraform object %s from Secret %s...", terraform.GetName(), outputsSecretName))

	outputsSecret := &corev1.Secret{}
	if err := provisioner.client.Get(ctx, types.NamespacedName{Name: outputsSecretName, Namespace: terraform.GetNamespace()}, outputsSecret); err != nil {
		return nil, fmt.Errorf("unable to find outputs secret %s: %w", outputsSecretName, err)
	}

	provisioner.log.Info(fmt.Sprintf("secret %s found. Trying to read...", outputsSecretName))

	outputsAvailable, exists, err := unstructured.NestedStringSlice(terraform.Object, "status", "availableOutputs")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	provisioner.log.Info(fmt.Sprintf("outputs available from Terraform object %s are: %s", terraform.GetName(), outputsAvailable))

	outputs := make(map[string]any)
	for _, outputName := range outputsAvailable {
		if rawValue, ok := outputsSecret.Data[outputName]; ok {
			// value, err := base64.StdEncoding.DecodeString(string(rawValue))
			// if err != nil {
			// 	return nil, err
			// }
			outputs[outputName] = string(rawValue)
		}
	}

	return outputs, nil
}
