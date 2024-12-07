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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	"github.com/nubank/klaudio/internal/resource"
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
func (r *ResourceGroupDeploymentReconciler) Reconcile(ctx context.Context, resourceGroupDeployment *resourcesv1alpha1.ResourceGroupDeployment) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("resourceGroupDeployment", resourceGroupDeployment.Name)

	knownResources := &resource.ResourceGroup{}

	// step 1: traverse all resources to determine relationship between them
	for _, candidate := range resourceGroupDeployment.Spec.Resources {
		log = log.WithValues("resource", candidate.Name)

		// every resource must reference a ResourceRef object
		resourceRef := &resourcesv1alpha1.ResourceRef{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: candidate.ResourceRef}, resourceRef); err != nil {
			log.Error(err, "unable to fetch ResourceRef", "resourceRef", candidate.Name)
			return ctrl.Result{}, err
		}

		resource, err := knownResources.Add(candidate.Name, candidate.Properties)
		if err != nil {
			log.Error(err, fmt.Sprintf("unable to unmarshal resource %s", candidate.Name), "resourceRef", candidate.Name)
			return ctrl.Result{}, err
		}

		resource.Ref = resourceRef
	}

	// step 2: generate a dag
	dag, err := knownResources.Graph()
	if err != nil {
		log.Error(err, "unable to generate a graph from deployment resources")
		return ctrl.Result{}, err
	}

	// step3: in order, expand and generate each resource
	for _, element := range dag {
		resource, err := knownResources.Get(element)
		if err != nil {
			return ctrl.Result{}, err
		}

	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceGroupDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ResourceGroupDeployment{}).
		Complete(reconcile.AsReconciler[*resourcesv1alpha1.ResourceGroupDeployment](mgr.GetClient(), r))
}
