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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
)

// ResourceGroupReconciler reconciles a ResourceGroup object
type ResourceGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcegroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcegroups/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ResourceGroup object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ResourceGroupReconciler) Reconcile(ctx context.Context, resourceGroup *resourcesv1alpha1.ResourceGroup) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("resourceGroup", resourceGroup.Name)

	if len(resourceGroup.Status.Conditions) == 0 {
		resourceGroupWithCondition, err := r.newResourceGroupCondition(ctx, resourceGroup, &metav1.Condition{
			Type:    resourcesv1alpha1.ResourceGroupConditionReady,
			Status:  metav1.ConditionUnknown,
			Reason:  resourcesv1alpha1.ResourceGroupConditionReasonReconciling,
			Message: fmt.Sprintf("Starting reconciliation from ResourceGroup %s", resourceGroup.Name),
		})
		if err != nil {
			log.Error(err, "Failed to update ResourceGroup status")
			return ctrl.Result{}, err
		}
		resourceGroup = resourceGroupWithCondition
	}

	// step 1: generate a dedicated namespace to resource group
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: resourceGroup.Name}, namespace); err != nil {
		if !apierrors.IsNotFound(err) {
			log.Error(err, "unable to fetch ResourceGroup's namespace")
			return ctrl.Result{}, err
		}

		log.Info(fmt.Sprintf("there is no namespace to ResourceGroup %s; trying to generate...", resourceGroup.Name))

		namespace.Name = resourceGroup.Name
		namespace.Labels = map[string]string{
			resourcesv1alpha1.Group + "/managedBy.group":   resourceGroup.GroupVersionKind().Group,
			resourcesv1alpha1.Group + "/managedBy.version": resourceGroup.GroupVersionKind().Version,
			resourcesv1alpha1.Group + "/managedBy.kind":    resourceGroup.GroupVersionKind().Kind,
			resourcesv1alpha1.Group + "/managedBy.name":    resourceGroup.Name,
		}
		if err := ctrl.SetControllerReference(resourceGroup, namespace, r.Scheme); err != nil {
			log.Error(err, "unable to set namespace's ownerReference", "namespace", namespace.Name)
			return ctrl.Result{}, err
		}

		if err := r.Create(ctx, namespace); err != nil {
			log.Error(err, fmt.Sprintf("unable to create namespace %s", namespace.Name), "namespace", namespace.Name)

			_, err = r.newResourceGroupCondition(ctx, resourceGroup, &metav1.Condition{
				Type:    resourcesv1alpha1.ResourceGroupConditionReady,
				Status:  metav1.ConditionFalse,
				Reason:  resourcesv1alpha1.ResourceGroupConditionReasonNamespaceCreationFailed,
				Message: fmt.Sprintf("Unable to create a namespace to ResourceGroup %s", resourceGroup.Name),
			})
			if err != nil {
				log.Error(err, "failed to update ResourceGroup's status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{Requeue: false}, err
		}

		log.Info(fmt.Sprintf("a namespace was created to ResourceGroup %s", resourceGroup.Name))
	}

	namespacedLog := log.WithValues("resourceGroupNamespace", namespace.Name)

	knowPlacements := sets.NewString()

	// step 1: traverse all resources and collect deployment placements
	for _, resource := range resourceGroup.Spec.Resources {
		// every resource must reference a ResourceRef object
		resourceRef := &resourcesv1alpha1.ResourceRef{}
		if err := r.Get(ctx, types.NamespacedName{Name: resource.ResourceRef}, resourceRef); err != nil {
			namespacedLog.Error(err, "unable to fetch ResourceRef", "resourceRef", resource.ResourceRef)
			return ctrl.Result{Requeue: false}, nil
		}

		knowPlacements = knowPlacements.Insert(resourceRef.Status.Placements...)
	}

	knowDeployments := make(resourcesv1alpha1.ResourceGroupDeploymentStatuses)

	// step 2: generate one ResourceGroupDeployment to each placement
	for _, placement := range knowPlacements.List() {
		resourceGroupDeployment := &resourcesv1alpha1.ResourceGroupDeployment{}

		deploymentLog := namespacedLog.WithValues("deployment", placement, "placement", placement)

		deploymentName := fmt.Sprintf("%s.%s", resourceGroup.Name, placement)

		if err := r.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: namespace.Name}, resourceGroupDeployment); err != nil {
			if !apierrors.IsNotFound(err) {
				deploymentLog.Error(err, "unable to fetch ResourceGroupDeployment")
				return ctrl.Result{}, err
			}

			// ResourceGroupDeployment does not exist yet; just create it
			resourceGroupDeployment.Name = deploymentName
			resourceGroupDeployment.Labels = map[string]string{
				resourcesv1alpha1.Group + "/managedBy.group":   resourceGroup.GroupVersionKind().Group,
				resourcesv1alpha1.Group + "/managedBy.version": resourceGroup.GroupVersionKind().Version,
				resourcesv1alpha1.Group + "/managedBy.kind":    resourceGroup.GroupVersionKind().Kind,
				resourcesv1alpha1.Group + "/managedBy.name":    resourceGroup.Name,
				resourcesv1alpha1.Group + "/placement":         placement,
			}
			resourceGroupDeployment.Namespace = namespace.Name
			resourceGroupDeployment.Spec.Placement = placement
			resourceGroupDeployment.Spec.Resources = resourceGroup.Spec.Resources

			if err := ctrl.SetControllerReference(resourceGroup, resourceGroupDeployment, r.Scheme); err != nil {
				deploymentLog.Error(err, "unable to set ResourceGroupDeployment's ownerReference")
				return ctrl.Result{}, err
			}

			if err := r.Create(ctx, resourceGroupDeployment); err != nil {
				deploymentLog.Error(err, fmt.Sprintf("unable to create ResourceGroupDeployment %s", resourceGroupDeployment.Name))
				return ctrl.Result{}, err
			}

			deploymentLog.Info(fmt.Sprintf("ResourceGroupDeployment to placement %s was created", placement))

			resourceGroupDeployment.Status.Phase = resourcesv1alpha1.DeploymentRunningPhase

		} else {
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				if err = r.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: namespace.Name}, resourceGroupDeployment); err != nil {
					return err
				}
				resourceGroupDeployment.Spec.Placement = placement
				resourceGroupDeployment.Spec.Resources = resourceGroup.Spec.Resources
				return r.Update(ctx, resourceGroupDeployment)
			})
			if err != nil {
				deploymentLog.Error(err, fmt.Sprintf("unable to update ResourceGroupDeployment %s", resourceGroupDeployment.Name))
				return ctrl.Result{}, err
			}
		}

		knowDeployments[resourceGroupDeployment.Name] = resourceGroupDeployment.Status
	}

	currentGroupPhase := resourcesv1alpha1.ResourceGroupDeploymentDonePhase
	for _, knowDeployment := range knowDeployments {
		if knowDeployment.Phase == resourcesv1alpha1.DeploymentRunningPhase {
			currentGroupPhase = resourcesv1alpha1.ResourceGroupDeploymentInProgressPhase
			break
		}
	}

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// refresh ResourceGroup
		if err := r.Get(ctx, types.NamespacedName{Name: resourceGroup.Name}, resourceGroup); err != nil {
			log.Error(err, "unable to refresh ResourceGroup")
			return err
		}
		resourceGroup.Status.Deployments = knowDeployments
		resourceGroup.Status.Phase = currentGroupPhase

		reason := resourcesv1alpha1.ResourceGroupConditionReasonDeploymentInProgress
		if currentGroupPhase == resourcesv1alpha1.ResourceGroupDeploymentDonePhase {
			reason = resourcesv1alpha1.ResourceGroupConditionReasonDeploymentDone
		}

		_, err := r.newResourceGroupCondition(ctx, resourceGroup, &metav1.Condition{
			Type:    resourcesv1alpha1.ResourceGroupConditionReady,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: fmt.Sprintf("All deployments from ResourceGroup %s were successfully scheduled", resourceGroup.Name),
		})

		return err
	})
	if err != nil {
		namespacedLog.Error(err, "unable to update ResourceGroups's status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ResourceGroupReconciler) newResourceGroupCondition(ctx context.Context, resourceGroup *resourcesv1alpha1.ResourceGroup, newCondition *metav1.Condition) (*resourcesv1alpha1.ResourceGroup, error) {
	meta.SetStatusCondition(&resourceGroup.Status.Conditions, *newCondition)
	if err := r.Status().Update(ctx, resourceGroup); err != nil {
		return nil, err
	}
	if err := r.Get(ctx, types.NamespacedName{Namespace: resourceGroup.Namespace, Name: resourceGroup.Name}, resourceGroup); err != nil {
		return nil, err
	}
	return resourceGroup, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ResourceGroup{}).
		Owns(&resourcesv1alpha1.ResourceGroupDeployment{}).
		Complete(reconcile.AsReconciler[*resourcesv1alpha1.ResourceGroup](mgr.GetClient(), r))
}
