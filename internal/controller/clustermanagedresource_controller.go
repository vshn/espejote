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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
)

// ClusterManagedResourceReconciler reconciles a ClusterManagedResource object
type ClusterManagedResourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Config *rest.Config
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterManagedResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&espejov1alpha1.ClusterManagedResource{}).
		Named("clustermanagedresource").
		Complete(r)
}

// +kubebuilder:rbac:groups=espejo.appuio.io,resources=clustermanagedresources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=espejo.appuio.io,resources=clustermanagedresources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=espejo.appuio.io,resources=clustermanagedresources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ClusterManagedResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// l := log.FromContext(ctx)

	mr := &espejov1alpha1.ClusterManagedResource{}
	if err := r.Get(ctx, req.NamespacedName, mr); err != nil {
		return ctrl.Result{}, err
	}

	return reconcileAsParser(ctx, r.Config, mr)
}
