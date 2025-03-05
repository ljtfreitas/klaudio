package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/gobuffalo/flect"
	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const CrossplaneProvisionerName = "crossplane"

type CrossplaneProvisioner struct {
	client        client.Client
	dynamicClient *dynamic.DynamicClient
	scheme        *runtime.Scheme
	log           logr.Logger
	properties    *crossplaneProvisionerProperties
}

type crossplaneProvisionerProperties struct {
	ObjectRef crossplaneProvisionerObjectRefProperties `json:"objectRef"`
}

type crossplaneProvisionerObjectRefProperties struct {
	ApiVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

func newCrossplaneProvisioner(c client.Client, d *dynamic.DynamicClient, scheme *runtime.Scheme, log logr.Logger, provisioner *resourcesv1alpha1.ResourceRefProvisioner) (Provisioner, error) {
	properties := &crossplaneProvisionerProperties{}
	if err := json.Unmarshal(provisioner.Properties.Raw, properties); err != nil {
		return nil, err
	}

	crossplaneProvisioner := &CrossplaneProvisioner{
		client:        c,
		dynamicClient: d,
		scheme:        scheme,
		log:           log,
		properties:    properties,
	}

	return crossplaneProvisioner, nil
}

func (provisioner *CrossplaneProvisioner) Run(ctx context.Context, resource *resourcesv1alpha1.Resource) (*ProvisionedResourceStatus, error) {
	provisioner.log.Info(fmt.Sprintf("starting Crossplane provisioner to resource %s/%s...", resource.Namespace, resource.Name))

	obj, err := provisioner.getOrNewObj(ctx, resource)
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("Crossplane object %s/%s has been created", obj.GetKind(), obj.GetName()))

	objStatus, err := status.Compute(obj)
	if err != nil {
		return nil, err
	}

	provisioner.log.Info(fmt.Sprintf("status from %s object %s is: %+v", obj.GetKind(), obj.GetName(), objStatus))

	provisionedResource := &ProvisionedResource{
		GroupVersionKind: obj.GroupVersionKind(),
		Name:             resource.Name,
	}

	switch objStatus.Status {

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

	outputs := make(map[string]any)
	atProvider, exists, err := unstructured.NestedMap(obj.Object, "status", "atProvider")
	if err != nil {
		return nil, err
	}
	if exists {
		outputs = atProvider
	}

	conditions, exists, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil {
		return nil, err
	}

	if exists {
		for _, condition := range conditions {
			conditionAsMap := condition.(map[string]any)

			conditionType := conditionAsMap["type"].(string)
			conditionStatus := conditionAsMap["status"].(string)
			if conditionType == "Ready" && conditionStatus == string(corev1.ConditionTrue) {
				status := &ProvisionedResourceStatus{
					Resource: provisionedResource,
					State:    ProvisionedResourceSuccessState,
					Outputs:  outputs,
				}
				return status, nil
			}
		}
	}

	provisioner.log.Info(fmt.Sprintf("can't determine Crossplane provisioning status for object %s/%s yet; keep running...", obj.GetKind(), obj.GetName()))

	resourceStatus := &ProvisionedResourceStatus{
		Resource: provisionedResource,
		State:    ProvisionedResourceRunningState,
		Outputs:  outputs,
	}

	return resourceStatus, nil
}

func (provisioner *CrossplaneProvisioner) getOrNewObj(ctx context.Context, resource *resourcesv1alpha1.Resource) (*unstructured.Unstructured, error) {
	objGv, err := schema.ParseGroupVersion(provisioner.properties.ObjectRef.ApiVersion)
	if err != nil {
		return nil, err
	}

	nameAsPlural := flect.Pluralize(provisioner.properties.ObjectRef.Kind)
	objResourceName := strings.ToLower(nameAsPlural)

	objGvWithResource := objGv.WithResource(objResourceName)

	obj, err := provisioner.dynamicClient.
		Resource(objGvWithResource).
		Namespace(resource.Namespace).
		Get(ctx, resource.Name, metav1.GetOptions{})

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		obj = &unstructured.Unstructured{}
		obj.SetGroupVersionKind(objGv.WithKind(provisioner.properties.ObjectRef.Kind))

		specProperties := make(map[string]any)
		if err := json.Unmarshal(resource.Spec.Properties.Raw, &specProperties); err != nil {
			return nil, err
		}

		content := make(map[string]any)
		content["apiVersion"] = provisioner.properties.ObjectRef.ApiVersion
		content["kind"] = provisioner.properties.ObjectRef.Kind
		content["metadata"] = map[string]any{
			"name":      resource.Name,
			"namespace": resource.Namespace,
		}
		content["spec"] = specProperties

		obj.SetUnstructuredContent(content)

		resourceGkv, err := apiutil.GVKForObject(resource, provisioner.scheme)
		if err != nil {
			return nil, err
		}

		obj.SetLabels(map[string]string{
			"name":      resource.Name,
			"namespace": resource.Namespace,
			resourcesv1alpha1.Group + "/managedBy.group":   resourceGkv.Group,
			resourcesv1alpha1.Group + "/managedBy.version": resourceGkv.Version,
			resourcesv1alpha1.Group + "/managedBy.kind":    resourceGkv.Kind,
			resourcesv1alpha1.Group + "/managedBy.name":    resource.Name,
			resourcesv1alpha1.Group + "/placement":         resource.Spec.Placement,
		})
		obj.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         resourceGkv.GroupVersion().String(),
				Kind:               resourceGkv.Kind,
				Name:               resource.Name,
				UID:                resource.UID,
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			},
		})

		if err := provisioner.client.Create(ctx, obj); err != nil {
			return nil, err
		}
	}

	return obj, nil
}
