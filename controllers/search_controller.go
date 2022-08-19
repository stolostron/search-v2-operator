/*
Copyright 2022.

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

package controllers

import (
	"context"
	"sync"
	"time"

	"github.com/stolostron/search-v2-operator/addon"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// SearchReconciler reconciles a Search object
type SearchReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const searchFinalizer = "search.open-cluster-management.io/finalizer"

var log = logf.Log.WithName("searchoperator")
var once sync.Once

//+kubebuilder:rbac:groups=search.open-cluster-management.io,resources=searches,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=search.open-cluster-management.io,resources=searches/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=search.open-cluster-management.io,resources=searches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Search object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *SearchReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	instance := &searchv1alpha1.Search{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: "search-v2-operator", Namespace: req.Namespace}, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Do not reconcile objects if this instance of search is labeled "paused"
	if IsPaused(instance.GetAnnotations()) {
		log.Info("Reconciliation is paused because the annotation 'search-pause: true' was found.")
		return ctrl.Result{}, nil
	}

	// Setup finalizers
	deleted, err := r.setFinalizer(ctx, instance)
	if err != nil || deleted {
		if deleted {
			log.V(2).Info("Search Instance deleted, reque request", err)
		} else {
			log.V(2).Info("Error setting Finalizer, reque request ", err)
		}
		return ctrl.Result{}, err
	}

	if instance.Spec.DBStorage.StorageClassName != "" && !r.isPVCPresent(ctx, instance) {

		pvcConfigured := r.configurePVC(ctx, instance)
		if !pvcConfigured {
			log.Info("Persistent Volume Claim is not ready yet , retyring in 10 seconds")
			return ctrl.Result{
				RequeueAfter: 10 * time.Second,
				Requeue:      true,
			}, nil
		}
	}
	result, err := r.createSearchServiceAccount(ctx, r.SearchServiceAccount(instance))
	if result != nil {
		log.Error(err, "SearchServiceAccount  setup failed")
		return *result, err
	}
	result, err = r.createRoles(ctx, r.ClusterRole(instance))
	if result != nil {
		log.Error(err, "ClusterRole  setup failed")
		return *result, err
	}
	result, err = r.createRoles(ctx, r.AddonClusterRole(instance))
	if result != nil {
		log.Error(err, "AddonClusterRole  setup failed")
		return *result, err
	}
	result, err = r.createClusterManagementAddOn(ctx, instance)
	if result != nil {
		log.Error(err, "ClusterManagementAddOn  setup failed")
		return *result, err
	}
	result, err = r.createRoleBinding(ctx, r.ClusterRoleBinding(instance))
	if result != nil {
		log.Error(err, "ClusterRoleBinding  setup failed")
		return *result, err
	}
	result, err = r.createSecret(ctx, r.PGSecret(instance))
	if result != nil {
		log.Error(err, "Postgres Secret  setup failed")
		return *result, err
	}
	result, err = r.createService(ctx, r.PGService(instance))
	if result != nil {
		log.Error(err, "Postgres Service  setup failed")
		return *result, err
	}
	result, err = r.createOrUpdateDeployment(ctx, r.PGDeployment(instance))
	if result != nil {
		log.Error(err, "Postgres Deployment  setup failed")
		return *result, err
	}

	result, err = r.createService(ctx, r.IndexerService(instance))
	if result != nil {
		log.Error(err, "Indexer Service  setup failed")
		return *result, err
	}
	result, err = r.createService(ctx, r.APIService(instance))
	if result != nil {
		log.Error(err, "API Service  setup failed")
		return *result, err
	}
	result, err = r.createOrUpdateDeployment(ctx, r.CollectorDeployment(instance))
	if result != nil {
		log.Error(err, "Collector Deployment  setup failed")
		return *result, err
	}
	result, err = r.createOrUpdateDeployment(ctx, r.IndexerDeployment(instance))
	if result != nil {
		log.Error(err, "Indexer Deployment  setup failed")
		return *result, err
	}
	result, err = r.createOrUpdateDeployment(ctx, r.APIDeployment(instance))
	if result != nil {
		log.Error(err, "API Deployment  setup failed")
		return *result, err
	}
	result, err = r.createConfigMap(ctx, r.IndexerConfigmap(instance))
	if result != nil {
		log.Error(err, "Indexer configmap  setup failed")
		return *result, err
	}
	result, err = r.createConfigMap(ctx, r.PostgresConfigmap(instance))
	if result != nil {
		log.Error(err, "Postgres configmap  setup failed")
		return *result, err
	}
	result, err = r.createConfigMap(ctx, r.SearchCACert(instance))
	if result != nil {
		log.Error(err, "Search CACert  setup failed")
		return *result, err
	}

	once.Do(func() {

		addon.CreateAddonOnce(ctx, instance)
	})

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SearchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&searchv1alpha1.Search{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Service{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Complete(r)
}

func (r *SearchReconciler) finalizeSearch(instance *searchv1alpha1.Search) error {
	err := r.deleteClusterManagementAddon(instance)
	if err != nil {
		return err
	}
	log.Info("Successfully finalized search")
	return nil
}

func (r *SearchReconciler) setFinalizer(ctx context.Context, instance *searchv1alpha1.Search) (bool, error) {
	// Check if the Search instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isSearchMarkedToBeDeleted := instance.GetDeletionTimestamp() != nil
	if isSearchMarkedToBeDeleted {
		log.V(2).Info("Search marked for deletion")
		if controllerutil.ContainsFinalizer(instance, searchFinalizer) {
			// Run finalization logic for searchFinalizer. If the
			// finalization logic fails, don't remove the finalizer so
			// that we can retry during the next reconciliation.
			if err := r.finalizeSearch(instance); err != nil {
				return false, err
			}
			// Remove searchFinalizer. Once all finalizers have been
			// removed, the object will be deleted.
			controllerutil.RemoveFinalizer(instance, searchFinalizer)
			r.Update(ctx, instance)
			log.Info("Finalizer removed from search CR")
		}
		return true, nil
	}
	// Add finalizer for this CR
	if !controllerutil.ContainsFinalizer(instance, searchFinalizer) {
		log.Info("Adding Finalizer to search CR")
		controllerutil.AddFinalizer(instance, searchFinalizer)
		err := r.Update(ctx, instance)
		if err != nil {
			log.V(2).Info("Error updating instance with Finalizer", err)
			return false, err
		}
	}
	return false, nil
}
