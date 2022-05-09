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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// SearchReconciler reconciles a Search object
type SearchReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

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
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Do not reconcile objects if this instance of search is labeled "paused"
	if IsPaused(instance.GetAnnotations()) {
		log.Info("Reconciliation is paused because the annotation 'search-pause: true' was found.")
		return ctrl.Result{}, nil
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
		Owns(&addonv1alpha1.ClusterManagementAddOn{}).
		Complete(r)
}
