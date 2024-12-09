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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
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

	// step 1: generate a dedicated namespace to resource group
	namespace := &corev1.Namespace{}
	if err := r.Client.Get(ctx, types.NamespacedName{Name: resourceGroup.Name}, namespace); err != nil {
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

		if err := r.Client.Create(ctx, namespace); err != nil {
			log.Error(err, fmt.Sprintf("unable to create namespace %s", namespace.Name), "namespace", namespace.Name)
			return ctrl.Result{}, err
		}

		log.Info(fmt.Sprintf("a namespace was created to ResourceGroup %s", resourceGroup.Name))
	}

	log = log.WithValues("resourceGroupNamespace", namespace.Name)

	knowPlacements := sets.NewString()

	// step 1: traverse all resources and collect deployment placements
	for _, resource := range resourceGroup.Spec.Resources {
		l := log.WithValues("resource", resource.Name)

		// every resource must reference a ResourceRef object
		resourceRef := &resourcesv1alpha1.ResourceRef{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: resource.ResourceRef}, resourceRef); err != nil {
			l.Error(err, "unable to fetch ResourceRef", "resourceRef", resource.Name)
			return ctrl.Result{}, err
		}

		knowPlacements = knowPlacements.Insert(resourceRef.Status.Placements...)
	}

	knowDeployments := make(resourcesv1alpha1.ResourceGroupDeploymentStatuses)

	// step 2: generate one ResourceGroupDeployment to each placement
	for _, placement := range knowPlacements.List() {
		resourceGroupDeployment := &resourcesv1alpha1.ResourceGroupDeployment{}

		l := log.WithValues("deployment", placement, "placement", placement)

		deploymentName := fmt.Sprintf("%s.%s", resourceGroup.Name, placement)

		if err := r.Client.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: namespace.Name}, resourceGroupDeployment); err != nil {
			if !apierrors.IsNotFound(err) {
				l.Error(err, "unable to fetch ResourceGroupDeployment")
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
			resourceGroupDeployment.Spec = resourcesv1alpha1.ResourceGroupDeploymentSpec{
				Placement: placement,
				Resources: resourceGroup.Spec.Resources,
			}
			resourceGroupDeployment.Status = resourcesv1alpha1.ResourceGroupDeploymentStatus{
				Status: resourcesv1alpha1.DeploymentStarted,
			}

			if err := ctrl.SetControllerReference(resourceGroup, resourceGroupDeployment, r.Scheme); err != nil {
				l.Error(err, "unable to set ResourceGroupDeployment's ownerReference")
				return ctrl.Result{}, err
			}

			if err := r.Client.Create(ctx, resourceGroupDeployment); err != nil {
				l.Error(err, fmt.Sprintf("unable to create namespace %s", namespace.Name), "namespace", namespace.Name)
				return ctrl.Result{}, err
			}

			l.Info(fmt.Sprintf("ResourceGroupDeployment to placement %s was created", placement))
		}

		knowDeployments[resourceGroupDeployment.Name] = resourceGroupDeployment.Status
	}

	resourceGroup.Status.Deployments = knowDeployments
	resourceGroup.Status.Status = resourcesv1alpha1.ResourceGroupDeploymentInProgress
	if err := r.Status().Update(ctx, resourceGroup); err != nil {
		log.Error(err, "unable to update ResourceGroups's status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ResourceGroup{}).
		Owns(&resourcesv1alpha1.ResourceGroupDeployment{}).
		Complete(reconcile.AsReconciler[*resourcesv1alpha1.ResourceGroup](mgr.GetClient(), r))
}
