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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	"github.com/nubank/klaudio/internal/refs"
	"github.com/nubank/klaudio/internal/resources"
)

// ResourceGroupDeploymentReconciler reconciles a ResourceGroupDeployment object
type ResourceGroupDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcegroupdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcegroupdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcegroupdeployments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ResourceGroupDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *ResourceGroupDeploymentReconciler) Reconcile(ctx context.Context, deployment *resourcesv1alpha1.ResourceGroupDeployment) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("resourceGroupDeployment", deployment.Name)

	if len(deployment.Status.Conditions) == 0 {
		deployment.Status.Phase = resourcesv1alpha1.DeploymentInProgressPhase
		deploymentWithCondition, err := r.newResourceGroupDeploymentCondition(ctx, deployment, &metav1.Condition{
			Type:    resourcesv1alpha1.ConditionTypeInitializing,
			Status:  metav1.ConditionUnknown,
			Reason:  resourcesv1alpha1.ConditionReasonReconciling,
			Message: fmt.Sprintf("Starting reconciliation from ResourceGroupDeployment %s", deployment.Name),
		})
		if err != nil {
			log.Error(err, "Failed to update ResourceGroupDeployment's status")
			return ctrl.Result{}, err
		}
		deployment = deploymentWithCondition
	}

	parameters := make(map[string]any)
	if deployment.Spec.Parameters != nil {
		if err := json.Unmarshal(deployment.Spec.Parameters.Raw, &parameters); err != nil {
			log.Error(err, "failed to deserialize deployment parameters")
			return ctrl.Result{Requeue: false}, err
		}
	}

	references := refs.NewReferences()

	// step 1: resolve references
	for _, ref := range deployment.Spec.Refs {
		referenceObject, err := references.NewReference(ctx, r.Client, ref)
		if err != nil {
			log.Error(err, "unable to fetch Ref", "ref", ref.Name)
			return ctrl.Result{}, err
		}

		log.Info(fmt.Sprintf("resolved reference: %+v", referenceObject))
	}

	resourceGroup := resources.NewResourceGroup()

	// step 2: traverse all resources to determine relationship between them
	for _, candidate := range deployment.Spec.Resources {
		logWithResource := log.WithValues("resource", candidate.Name)

		// every resource must reference a ResourceRef object
		resourceRef := &resourcesv1alpha1.ResourceRef{}
		if err := r.Get(ctx, types.NamespacedName{Name: candidate.ResourceRef}, resourceRef); err != nil {
			logWithResource.Error(err, "unable to fetch ResourceRef", "resourceRef", candidate.Name)
			return ctrl.Result{}, err
		}

		resource, err := resourceGroup.NewResource(candidate.Name, candidate.Properties)
		if err != nil {
			logWithResource.Error(err, fmt.Sprintf("unable to unmarshal resource %s", candidate.Name), "resourceRef", candidate.Name)
			return ctrl.Result{}, err
		}

		resource.Ref = resourceRef
	}

	// step 3: generate a dag
	dag, err := resourceGroup.Graph()
	if err != nil {
		log.Error(err, "unable to generate a graph from deployment resources")
		return ctrl.Result{}, err
	}

	log.Info(fmt.Sprintf("Generated dag: %s", dag))

	args := resources.NewResourcePropertiesArgs(parameters, references)

	knowResources := make(resourcesv1alpha1.ResourceGroupDeploymentResourcesStatuses)

	// step 4: in order, expand and generate each resource
	for _, resourceName := range dag {
		resource, err := resourceGroup.Get(resourceName)
		if err != nil {
			return ctrl.Result{}, err
		}
		logWithResource := log.WithValues("resource", resource.Name)

		log.Info(fmt.Sprintf("Processing %s...", resource.Name))

		// first, expand properties
		expandedProperties, err := resource.Evaluate(args)
		if err != nil {
			log.Error(err, "unable to evaluate properties")
			return ctrl.Result{}, err
		}

		rawProperties, err := json.Marshal(expandedProperties)
		if err != nil {
			log.Error(err, "unable to serialize resource properties")
			return ctrl.Result{}, err
		}

		resourceNameToDeploy := fmt.Sprintf("%s.%s", deployment.Name, resource.NameAsKebabCase())

		resourceToDeploy := &resourcesv1alpha1.Resource{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: deployment.Namespace, Name: resourceNameToDeploy}, resourceToDeploy); err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "unable to fetch Resource object")
				return ctrl.Result{}, err
			}

			// there is no Resource yet; just create it
			log.Info(fmt.Sprintf("Creating Resource %s...", resourceNameToDeploy))

			resourceToDeploy.Name = resourceNameToDeploy
			resourceToDeploy.Namespace = deployment.Namespace
			resourceToDeploy.Labels = map[string]string{
				resourcesv1alpha1.Group + "/managedBy.group":   deployment.GroupVersionKind().Group,
				resourcesv1alpha1.Group + "/managedBy.version": deployment.GroupVersionKind().Version,
				resourcesv1alpha1.Group + "/managedBy.kind":    deployment.GroupVersionKind().Kind,
				resourcesv1alpha1.Group + "/managedBy.name":    deployment.Name,
				resourcesv1alpha1.Group + "/placement":         deployment.Spec.Placement,
			}
			resourceToDeploy.Spec = resourcesv1alpha1.ResourceSpec{
				Placement:   deployment.Spec.Placement,
				ResourceRef: resource.Ref.Name,
				Properties:  &runtime.RawExtension{Raw: rawProperties},
			}
			if err := ctrl.SetControllerReference(deployment, resourceToDeploy, r.Scheme); err != nil {
				log.Error(err, "unable to set Resource's ownerReference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, resourceToDeploy); err != nil {
				logWithResource.Error(err, fmt.Sprintf("unable to schedule Resource %s to be deployed", resourceNameToDeploy))

				_, err = r.newResourceGroupDeploymentCondition(ctx, deployment, &metav1.Condition{
					Type:    resourcesv1alpha1.ConditionTypeFailed,
					Status:  metav1.ConditionFalse,
					Reason:  resourcesv1alpha1.ConditionReasonFailed,
					Message: fmt.Sprintf("Unable to schedule Resource %s to be deployed", resourceNameToDeploy),
				})

				return ctrl.Result{}, err
			}

			_, err = r.newResourceGroupDeploymentCondition(ctx, deployment, &metav1.Condition{
				Type:    resourcesv1alpha1.ConditionTypeInProgress,
				Status:  metav1.ConditionTrue,
				Reason:  resourcesv1alpha1.ConditionReasonDeploymentInProgress,
				Message: fmt.Sprintf("Resource %s, from ResourceGroupDeployment %s, was successfully scheduled to be deployed", resourceNameToDeploy, deployment.Name),
			})
			if err != nil {
				log.Error(err, "failed to update ResourceGroupDeployment's status")
				return ctrl.Result{}, err
			}

			logWithResource.Info(fmt.Sprintf("Resource %s scheduled to be deployed; deploy is in progress through reconciliation process", resourceNameToDeploy))

			// just reschedule the reconcilation
			return ctrl.Result{RequeueAfter: time.Duration(5) * time.Second}, nil
		} else {
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				if err = r.Get(ctx, types.NamespacedName{Name: resourceNameToDeploy, Namespace: deployment.Namespace}, resourceToDeploy); err != nil {
					return err
				}
				resourceToDeploy.Spec.Properties = &runtime.RawExtension{Raw: rawProperties}
				return r.Update(ctx, resourceToDeploy)
			})
			if err != nil {
				logWithResource.Error(err, fmt.Sprintf("unable to update spec properties from Resource %s", resourceNameToDeploy))

				_, err = r.newResourceGroupDeploymentCondition(ctx, deployment, &metav1.Condition{
					Type:    resourcesv1alpha1.ConditionTypeFailed,
					Status:  metav1.ConditionFalse,
					Reason:  resourcesv1alpha1.ConditionReasonFailed,
					Message: fmt.Sprintf("unable to update spec properties from Resource %s", resourceNameToDeploy),
				})

				return ctrl.Result{}, err
			}
		}

		// check the current deployment to resource
		if resourceToDeploy.Status.Phase == resourcesv1alpha1.DeploymentInProgressPhase {
			return ctrl.Result{RequeueAfter: time.Duration(5) * time.Second}, nil
		}

		// collect the resource to be used as argument and move to the next one
		args, err = args.WithResource(resource.Name, resourceToDeploy)
		if err != nil {
			log.Error(err, "failed to update ResourcePropertiesArgs map")
			return ctrl.Result{}, err
		}

		knowResources[resourceToDeploy.Name] = resourceToDeploy.Status
	}

	log.Info("Updating deployment status...")

	currentConditionType := resourcesv1alpha1.ConditionTypeReady
	currentDeploymentPhase := resourcesv1alpha1.DeploymentDonePhase
	for _, knowResource := range knowResources {
		if knowResource.Phase == resourcesv1alpha1.DeploymentFailedPhase {
			currentConditionType = resourcesv1alpha1.ConditionTypeFailed
			currentDeploymentPhase = resourcesv1alpha1.DeploymentFailedPhase
			break
		}
		if knowResource.Phase == resourcesv1alpha1.DeploymentInProgressPhase {
			currentConditionType = resourcesv1alpha1.ConditionTypeInProgress
			currentDeploymentPhase = resourcesv1alpha1.DeploymentInProgressPhase
			break
		}
	}

	deployment.Status.Resources = knowResources
	deployment.Status.Phase = resourcesv1alpha1.ResourceGroupDeploymentStatusPhase(currentDeploymentPhase)
	_, err = r.newResourceGroupDeploymentCondition(ctx, deployment, &metav1.Condition{
		Type:    currentConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  resourcesv1alpha1.StatusPhaseToReason(currentDeploymentPhase),
		Message: fmt.Sprintf("Resources from ResourceGroupDeployment %s were successfully scheduled to be deployed", deployment.Name),
	})
	if err != nil {
		log.Error(err, "Failed to update ResourceGroupDeployment's status")
		return ctrl.Result{}, err
	}

	if currentDeploymentPhase == resourcesv1alpha1.DeploymentDonePhase {
		log.Info("Deployment finished.")
		return ctrl.Result{}, nil
	}

	// reschedule the reconciliation until the deployment is done
	return ctrl.Result{RequeueAfter: time.Duration(5) * time.Second}, nil
}

func (r *ResourceGroupDeploymentReconciler) newResourceGroupDeploymentCondition(ctx context.Context, resourceGroupDeployment *resourcesv1alpha1.ResourceGroupDeployment, newCondition *metav1.Condition) (*resourcesv1alpha1.ResourceGroupDeployment, error) {
	meta.SetStatusCondition(&resourceGroupDeployment.Status.Conditions, *newCondition)
	if err := r.Status().Update(ctx, resourceGroupDeployment); err != nil {
		return nil, err
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: resourceGroupDeployment.Namespace, Name: resourceGroupDeployment.Name}, resourceGroupDeployment); err != nil {
		return nil, err
	}
	return resourceGroupDeployment, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceGroupDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ResourceGroupDeployment{}).
		Complete(reconcile.AsReconciler(mgr.GetClient(), r))
}
