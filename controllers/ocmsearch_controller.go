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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
)

// OCMSearchReconciler reconciles a OCMSearch object
type OCMSearchReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

var log = logf.Log.WithName("searchoperator")

//+kubebuilder:rbac:groups=cache.open-cluster-management.io,resources=ocmsearches,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cache.open-cluster-management.io,resources=ocmsearches/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cache.open-cluster-management.io,resources=ocmsearches/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the OCMSearch object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *OCMSearchReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	instance := &cachev1.OCMSearch{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "search-v2-operator", Namespace: req.Namespace}, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	result, err := r.createPGSecret(req, r.PGSecret(instance), instance)
	if result != nil {
		log.Error(err, "Postgres Secret  setup failed")
		return *result, err
	}
	result, err = r.createPGService(req, r.PGService(instance), instance)
	if result != nil {
		log.Error(err, "Postgres Service  setup failed")
		return *result, err
	}
	result, err = r.createPGDeployment(req, r.PGDeployment(instance), instance)
	if result != nil {
		log.Error(err, "Postgres Deployment  setup failed")
		return *result, err
	}
	result, err = r.createIndexerService(req, r.IndexerService(instance), instance)
	if result != nil {
		log.Error(err, "Indexer Service  setup failed")
		return *result, err
	}
	result, err = r.createAPIService(req, r.APIService(instance), instance)
	if result != nil {
		log.Error(err, "API Service  setup failed")
		return *result, err
	}
	result, err = r.createCollectorDeployment(req, r.CollectorDeployment(instance), instance)
	if result != nil {
		log.Error(err, "Collector Deployment  setup failed")
		return *result, err
	}
	result, err = r.createIndexerDeployment(req, r.IndexerDeployment(instance), instance)
	if result != nil {
		log.Error(err, "Indexer Deployment  setup failed")
		return *result, err
	}
	result, err = r.createAPIDeployment(req, r.APIDeployment(instance), instance)
	if result != nil {
		log.Error(err, "API Deployment  setup failed")
		return *result, err
	}
	result, err = r.createIndexerConfigmap(req, r.IndexerConfigmap(instance), instance)
	if result != nil {
		log.Error(err, "Indexer configmap  setup failed")
		return *result, err
	}
	result, err = r.createSearchCACert(req, r.SearchCACert(instance), instance)
	if result != nil {
		log.Error(err, "Search CACert  setup failed")
		return *result, err
	}

	return ctrl.Result{}, nil
}

func (r *OCMSearchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1.OCMSearch{}).
		Complete(r)
}
