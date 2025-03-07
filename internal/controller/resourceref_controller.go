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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
)

// ResourceRefReconciler reconciles a ResourceRef object
type ResourceRefReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcerefs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcerefs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=resources.klaudio.nubank.io,resources=resourcerefs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ResourceRef object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ResourceRefReconciler) Reconcile(ctx context.Context, resourceRef *resourcesv1alpha1.ResourceRef) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("resourceRef", resourceRef.Name)

	resourceRef.Status.Status = resourcesv1alpha1.ResourceRefStatusReady
	resourceRef.Status.Placements = []string{"account-1"}
	if err := r.Status().Update(ctx, resourceRef); err != nil {
		log.Error(err, "unable to update ResourceRef's status")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info(fmt.Sprintf("ResourceRef %s was updated", resourceRef.Name))

	r.Recorder.Eventf(resourceRef, "Normal", "Reconcile", "ResourceRef %s is reconciled.", resourceRef.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ResourceRefReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&resourcesv1alpha1.ResourceRef{}).
		Complete(reconcile.AsReconciler(mgr.GetClient(), r))
}
