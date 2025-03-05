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

	resourcesv1alpha1 "github.com/nubank/klaudio/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// NamespaceReconciler reconciles a Namespace object
type NamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	OpenTofuClusterRoleName    = "tf-runner-role"
	OpenTofuServiceAccountName = "tf-runner"

	OpenTofuRoleBindingName = "opentofu-runner"
)

// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=namespaces/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Namespace object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *NamespaceReconciler) Reconcile(ctx context.Context, namespace *corev1.Namespace) (ctrl.Result, error) {
	namespacedLog := log.FromContext(ctx).WithValues("namespace", namespace.Name)

	openTofuRunnerRoleBinding := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: OpenTofuRoleBindingName, Namespace: namespace.Name}, openTofuRunnerRoleBinding); err != nil {
		if !apierrors.IsNotFound(err) {
			namespacedLog.Error(err, "unable to fetch OpenTofu Runner's role binding")
			return ctrl.Result{}, err
		}

		namespacedLog.Info(fmt.Sprintf("there is no role binding to run OpenTofu in the namespace %s; trying to generate...", namespace.Name))

		openTofuRunnerRoleBinding.Name = OpenTofuRoleBindingName
		openTofuRunnerRoleBinding.Namespace = namespace.Name
		openTofuRunnerRoleBinding.RoleRef = rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     OpenTofuClusterRoleName,
		}
		openTofuRunnerRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      OpenTofuServiceAccountName,
				Namespace: namespace.Name,
			},
		}

		if err := r.Create(ctx, openTofuRunnerRoleBinding); err != nil {
			namespacedLog.Error(err, fmt.Sprintf("unable to create the required OpenTofu role binding in namespace %s", namespace.Name))
			return ctrl.Result{}, err
		}

		namespacedLog.Info(fmt.Sprintf("a RoleBinding to run OpenTofu runnners in namespace %s was created", namespace.Name))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	expectedLabel, err := predicate.LabelSelectorPredicate(v1.LabelSelector{
		MatchLabels: map[string]string{
			resourcesv1alpha1.Group + "/managedBy.group": resourcesv1alpha1.Group,
		},
	})
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(expectedLabel).
		For(&corev1.Namespace{}).
		Complete(reconcile.AsReconciler[*corev1.Namespace](mgr.GetClient(), r))
}
