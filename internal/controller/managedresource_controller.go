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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/internal/controller/converter"
	"github.com/vshn/espejote/internal/dynamic"
)

// ManagedResourceReconciler reconciles a ManagedResource object
type ManagedResourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	Config *rest.Config
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedResourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&espejov1alpha1.ManagedResource{}).
		Named("managedresource").
		Complete(r)
}

// +kubebuilder:rbac:groups=espejo.appuio.io,resources=managedresources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=espejo.appuio.io,resources=managedresources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=espejo.appuio.io,resources=managedresources/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ManagedResourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// l := log.FromContext(ctx)

	mr := &espejov1alpha1.ManagedResource{}
	if err := r.Get(ctx, req.NamespacedName, mr); err != nil {
		return ctrl.Result{}, err
	}

	return reconcileAsParser(ctx, r.Config, mr)
}

func reconcileAsParser(ctx context.Context, cfg *rest.Config, c converter.Converter) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	clt, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Create parser and execute template.
	p, err := converter.ToParser(ctx, clt, c)
	if err != nil {
		return ctrl.Result{}, nil
	}

	output, err := p.Parse()
	if err != nil {
		return ctrl.Result{}, nil
	}

	for _, o := range output {
		// Get resource client
		dr, err := clt.Resource(o.GetAPIVersion(), o.GetKind(), o.GetNamespace())
		if err != nil {
			return ctrl.Result{}, err
		}

		// Marshal object into JSON
		data, err := json.Marshal(o)
		if err != nil {
			return ctrl.Result{}, err
		}

		_, err = dr.Patch(ctx, o.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
			FieldManager: "espejote",
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		l.Error(err, "örrör")
		// if err := clt.ApplyObj(ctx, o); err != nil {
		// 	l.Error(err, "Error applying object")
		// }
	}

	return ctrl.Result{}, nil
}
