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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	"github.com/nubank/klaudio/internal/provisioning"
)

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
		resource.Status.Phase = resourcesv1alpha1.DeploymentInProgressPhase
		resourceWithCondition, err := r.newResourceCondition(ctx, resource, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeInProgress,
			Status:  metav1.ConditionUnknown,
			Reason:  resourcesv1alpha1.ConditionReasonReconciling,
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

	resourceRefProvisioner := resourceRef.Spec.Provisioner
	provisionerName := resourceRefProvisioner.Name

	logWithProvisioner := logWithResource.WithValues("provisioner", provisionerName)

	provisionerFactory, err := provisioning.SelectByName(string(provisionerName))
	if err != nil {
		logWithProvisioner.Error(err, fmt.Sprintf("unsupported ResourceRef provisioner: %s", resourceRefProvisioner))

		_, err := r.newResourceCondition(ctx, resource, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeFailed,
			Status:  metav1.ConditionFalse,
			Reason:  resourcesv1alpha1.ConditionReasonFailed,
			Message: fmt.Sprintf("Unsupported ResourceRef provisioner: %s", provisionerName),
		})

		return ctrl.Result{Requeue: false}, err
	}

	provisioner, err := provisionerFactory(r.Client, r.DynamicClient, r.Scheme, logWithProvisioner, &resourceRef.Spec.Provisioner)
	if err != nil {
		logWithProvisioner.Error(err, fmt.Sprintf("unsupported ResourceRef provisioner: %s; unable to create a Provisioner instance", provisionerName))

		_, err := r.newResourceCondition(ctx, resource, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeFailed,
			Status:  metav1.ConditionFalse,
			Reason:  resourcesv1alpha1.ConditionReasonFailed,
			Message: fmt.Sprintf("Unsupported ResourceRef provisioner: %s", provisionerName),
		})

		return ctrl.Result{Requeue: false}, err
	}

	logWithProvisioner.Info(fmt.Sprintf("Running provisioner: %s", provisionerName))

	status, err := provisioner.Run(ctx, resource)

	if err != nil {
		logWithProvisioner.Error(err, fmt.Sprintf("failed to run %s provisioner", provisionerName))

		_, err := r.newResourceCondition(ctx, resource, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeFailed,
			Status:  metav1.ConditionFalse,
			Reason:  resourcesv1alpha1.ConditionReasonFailed,
			Message: fmt.Sprintf("Failed to run provisioner: %s", provisionerName),
		})

		return ctrl.Result{Requeue: false}, err
	}

	logWithResource.Info(fmt.Sprintf("Current state from %s provisioning is %s", provisionerName, status.State))

	if status.IsRunning() {
		return ctrl.Result{RequeueAfter: time.Duration(5) * time.Second}, nil
	}

	phase, condition := statusToCondition(status, resource)

	resource.Status.Phase = resourcesv1alpha1.ResourceStatusDescription(phase)

	if status.Resource != nil {
		resource.Status.Provisioner = resourcesv1alpha1.ResourceStatusProvisioner{
			State: string(status.State),
			Resource: resourcesv1alpha1.ResourceStatusProvisionerResource{
				Group:   status.Resource.Group,
				Version: status.Resource.Version,
				Kind:    status.Resource.Kind,
				Name:    status.Resource.Name,
			},
		}
	}
	if status.Outputs != nil {
		outputAsJson, err := json.Marshal(status.Outputs)
		if err != nil {
			logWithResource.Error(err, "failed to unmarshall provisioned resource outputs")
			return ctrl.Result{Requeue: false}, err
		}
		resource.Status.Outputs = &runtime.RawExtension{Raw: outputAsJson}
	}

	_, err = r.newResourceCondition(ctx, resource, condition)
	if err != nil {
		logWithResource.Error(err, "Failed to update Resource's status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func statusToCondition(status *provisioning.ProvisionedResourceStatus, resource *resourcesv1alpha1.Resource) (string, *metav1.Condition) {
	switch status.State {
	case provisioning.ProvisionedResourceSuccessState:
		return resourcesv1alpha1.DeploymentDonePhase, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  resourcesv1alpha1.ConditionReasonDeploymentDone,
			Message: fmt.Sprintf("Deployment from Resource %s was successfully finished", resource.Name),
		}
	case provisioning.ProvisionedResourceFailedState:
		return resourcesv1alpha1.DeploymentFailedPhase, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeFailed,
			Status:  metav1.ConditionFalse,
			Reason:  resourcesv1alpha1.ConditionReasonDeploymentFailed,
			Message: fmt.Sprintf("Deployment from Resource %s failed", resource.Name),
		}
	default:
		return resourcesv1alpha1.DeploymentInProgressPhase, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionUnknown,
			Reason:  resourcesv1alpha1.ConditionReasonDeploymentInProgress,
			Message: fmt.Sprintf("Deployment from Resource %s is running...", resource.Name),
		}
	}
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
