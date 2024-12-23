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

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
)

type ProvisionerStatusStateDescription string

const (
	ProvisionerStatusRunningState = ProvisionerStatusStateDescription("Running")
	ProvisionerStatusFailedState  = ProvisionerStatusStateDescription("Failed")
	ProvisionerStatusSuccessState = ProvisionerStatusStateDescription("Success")
)

type ProvisionerStatus struct {
	Resource *ProvisionerStatusResource
	State    ProvisionerStatusStateDescription
	Outputs  map[string]any
}

type ProvisionerStatusResource struct {
	schema.GroupVersionKind
	Name string
}

func (p *ProvisionerStatus) IsRunning() bool {
	return p.State == ProvisionerStatusRunningState
}

// ResourceReconciler reconciles a Resource object
type ResourceReconciler struct {
	client.Client
	*dynamic.DynamicClient
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Resource object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ResourceReconciler) Reconcile(ctx context.Context, resource *resourcesv1alpha1.Resource) (ctrl.Result, error) {
	logWithResource := log.FromContext(ctx).WithValues("resource", resource.Name)

	if len(resource.Status.Conditions) == 0 {
		resource.Status.Phase = resourcesv1alpha1.ResourceDeployingStatusPhase
		resourceWithCondition, err := r.newResourceCondition(ctx, resource, &metav1.Condition{
			Type:    resourcesv1alpha1.ResourceConditionReady,
			Status:  metav1.ConditionUnknown,
			Reason:  resourcesv1alpha1.ResourceConditionReasonReconciling,
			Message: fmt.Sprintf("Starting reconciliation from Resource %s", resource.Name),
		})
		if err != nil {
			logWithResource.Error(err, "Failed to update Resource's status")
			return ctrl.Result{}, err
		}
		resource = resourceWithCondition
	}

	resourceRef := &resourcesv1alpha1.ResourceRef{}
	if err := r.Get(ctx, types.NamespacedName{Name: resource.Spec.ResourceRef}, resourceRef); err != nil {
		logWithResource.Error(err, "unable to fetch ResourceRef", "resourceRef", resource.Name)
		return ctrl.Result{Requeue: false}, nil
	}

	provisioner := resourceRef.Spec.Provisioner

	logWithResource.Info(fmt.Sprintf("Running with provisioner: %s", provisioner.Name))

	switch provisioner.Name {
	case resourcesv1alpha1.ResourceRefPulumiProvisioner:
		logWithResource.Info("Running Pulumi provisioner...")

		provisionerStatus, err := r.runPulumiProvisioner(ctx, resource)
		if err != nil {
			logWithResource.Error(err, "failed to run Pulumi provisioner")
			return ctrl.Result{Requeue: false}, err
		}

		logWithResource.Info(fmt.Sprintf("Current state from Pulumi provisioning is %s", provisionerStatus.State))

		if provisionerStatus.IsRunning() {
			return ctrl.Result{RequeueAfter: time.Duration(5) * time.Second}, nil
		}

		var condition *metav1.Condition
		switch provisionerStatus.State {
		case ProvisionerStatusRunningState:
			resource.Status.Phase = resourcesv1alpha1.ResourceDeployingStatusPhase
			condition = &metav1.Condition{
				Type:    resourcesv1alpha1.ResourceConditionReady,
				Status:  metav1.ConditionUnknown,
				Reason:  resourcesv1alpha1.ResourceConditionReasonDeploymentInProgress,
				Message: fmt.Sprintf("Deployment from Resource %s is running...", resource.Name),
			}
		case ProvisionerStatusSuccessState:
			resource.Status.Phase = resourcesv1alpha1.ResourceDoneStatusPhase
			condition = &metav1.Condition{
				Type:    resourcesv1alpha1.ResourceConditionReady,
				Status:  metav1.ConditionTrue,
				Reason:  resourcesv1alpha1.ResourceConditionReasonDeploymentDone,
				Message: fmt.Sprintf("Deployment from Resource %s was successfully finished", resource.Name),
			}
		case ProvisionerStatusFailedState:
			resource.Status.Phase = resourcesv1alpha1.ResourceFailedStatusPhase
			condition = &metav1.Condition{
				Type:    resourcesv1alpha1.ResourceConditionReady,
				Status:  metav1.ConditionFalse,
				Reason:  resourcesv1alpha1.ResourceConditionReasonDeploymentFailed,
				Message: fmt.Sprintf("Deployment from Resource %s failed", resource.Name),
			}
		}

		if provisionerStatus.Resource != nil {
			resource.Status.Provisioner = resourcesv1alpha1.ResourceStatusProvisioner{
				State: string(provisionerStatus.State),
				Resource: resourcesv1alpha1.ResourceStatusProvisionerResource{
					Group:   provisionerStatus.Resource.Group,
					Version: provisionerStatus.Resource.Version,
					Kind:    provisionerStatus.Resource.Kind,
					Name:    provisionerStatus.Resource.Name,
				},
			}
		}
		if provisionerStatus.Outputs != nil {
			outputAsJson, err := json.Marshal(provisionerStatus.Outputs)
			if err != nil {
				logWithResource.Error(err, "failed to unmarshall Pulumi stack outputs")
				return ctrl.Result{Requeue: false}, err
			}
			resource.Status.Outputs = &runtime.RawExtension{Raw: outputAsJson}
		}

		_, err = r.newResourceCondition(ctx, resource, condition)
		if err != nil {
			logWithResource.Error(err, "Failed to update Resource's status")
			return ctrl.Result{}, err
		}

	default:
		logWithResource.Error(fmt.Errorf("unsupported ResourceRef provisioner: %s", provisioner.Name), fmt.Sprintf("unsupported ResourceRef provisioner: %s", provisioner.Name))
		return ctrl.Result{Requeue: false}, nil
	}

	return ctrl.Result{}, nil
}

func (r *ResourceReconciler) runPulumiProvisioner(ctx context.Context, resource *resourcesv1alpha1.Resource) (*ProvisionerStatus, error) {
	stackGkv := schema.GroupVersionKind{
		Group:   "pulumi.com",
		Version: "v1",
		Kind:    "Stack",
	}

	stackGkvWithResource := stackGkv.GroupVersion().WithResource("stacks")

	unstructuredObject, err := r.DynamicClient.
		Resource(stackGkvWithResource).
		Namespace(resource.Namespace).
		Get(ctx, resource.Name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		unstructuredObject = &unstructured.Unstructured{}
		unstructuredObject.SetGroupVersionKind(stackGkv)

		asJson := fmt.Sprintf(`{
			"apiVersion": "pulumi.com/v1",
			"kind": "Stack",
			"metadata": {
				"name": "%s",
				"namespace": "%s"
			},
			"spec": {
				"envRefs": {
					"PULUMI_CONFIG_PASSPHRASE": {
						"type": "Literal",
						"literal": {
							"value": ""
						}
					}
				},
				"gitAuth": {
					"accessToken": {
						"type": "Secret",
						"secret": {
							"name": "github-access-token",
							"namespace": "default",
							"key": "accessToken"
						}
					}
				},
				"stack": "%s",
				"projectRepo": "https://github.com/ljtfreitas/pulumi-sample-project.git",
				"branch": "main",
				"repoDir": "just-a-pet"
			}
		}`, resource.Name, resource.Namespace, fmt.Sprintf("%s.%s", resource.Spec.Placement, resource.Name))

		object := make(map[string]any)
		if err := json.Unmarshal([]byte(asJson), &object); err != nil {
			return nil, err
		}

		unstructuredObject.SetUnstructuredContent(object)

		resourceGkv, err := apiutil.GVKForObject(resource, r.Scheme)
		if err != nil {
			return nil, err
		}

		unstructuredObject.SetLabels(map[string]string{
			"name":      resource.Name,
			"namespace": resource.Namespace,
			resourcesv1alpha1.Group + "/managedBy.group":   resourceGkv.Group,
			resourcesv1alpha1.Group + "/managedBy.version": resourceGkv.Version,
			resourcesv1alpha1.Group + "/managedBy.kind":    resourceGkv.Kind,
			resourcesv1alpha1.Group + "/managedBy.name":    resource.Name,
			resourcesv1alpha1.Group + "/placement":         resource.Spec.Placement,
		})
		unstructuredObject.SetOwnerReferences([]metav1.OwnerReference{
			{
				APIVersion:         resourceGkv.GroupVersion().String(),
				Kind:               resourceGkv.Kind,
				Name:               resource.Name,
				UID:                resource.UID,
				BlockOwnerDeletion: ptr.To(true),
				Controller:         ptr.To(true),
			},
		})

		if err := r.Client.Create(ctx, unstructuredObject); err != nil {
			return nil, err
		}
	}

	stackStatus, exists, err := unstructured.NestedMap(unstructuredObject.Object, "status")
	if err != nil {
		return nil, err
	}

	provisionerResource := &ProvisionerStatusResource{
		GroupVersionKind: stackGkv,
		Name:             resource.Name,
	}

	if exists {
		if lastUpdate, exists := stackStatus["lastUpdate"].(map[string]any); exists {
			switch lastUpdate["state"] {

			case "succeeded":
				provisionerStatus := &ProvisionerStatus{
					Resource: provisionerResource,
					State:    ProvisionerStatusSuccessState,
					Outputs:  stackStatus["outputs"].(map[string]any),
				}
				return provisionerStatus, nil

			case "failed":
				provisionerStatus := &ProvisionerStatus{
					Resource: provisionerResource,
					State:    ProvisionerStatusFailedState,
					Outputs:  stackStatus["outputs"].(map[string]any),
				}
				return provisionerStatus, nil
			}
		}
	}

	provisionerStatus := &ProvisionerStatus{
		Resource: provisionerResource,
		State:    ProvisionerStatusRunningState,
		Outputs:  make(map[string]any),
	}

	return provisionerStatus, nil
}

func (r *ResourceReconciler) newResourceCondition(ctx context.Context, resource *resourcesv1alpha1.Resource, newCondition *metav1.Condition) (*resourcesv1alpha1.Resource, error) {
	meta.SetStatusCondition(&resource.Status.Conditions, *newCondition)
	if err := r.Status().Update(ctx, resource); err != nil {
		return nil, err
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: resource.Namespace, Name: resource.Name}, resource); err != nil {
		return nil, err
	}
	return resource, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.Resource{}).
		Complete(reconcile.AsReconciler(mgr.GetClient(), r))
}
