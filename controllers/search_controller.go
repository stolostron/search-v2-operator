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
	"strings"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stolostron/search-v2-operator/addon"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// SearchReconciler reconciles a Search object
type SearchReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	context       context.Context
	DynamicClient dynamic.Interface
}

const searchFinalizer = "search.open-cluster-management.io/finalizer"

var log = logf.Log.WithName("searchoperator")
var once sync.Once
var cleanOnce sync.Once

//+kubebuilder:rbac:groups=*,resources=*,verbs=list;get;watch
//+kubebuilder:rbac:groups="",resources=secrets;serviceaccounts;services,verbs=create;get;list;watch;patch;update;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;delete;get;list;patch;update;watch
// 'bind' on the pre-provisioned search-api and search-collector ClusterRoles lets the
// operator create their ClusterRoleBindings without holding 'impersonate' or wildcard read.
// Both ClusterRoles are static manifests; the operator never creates or updates them.
// The operator resolves the actual ClusterRole name at runtime via getAPIClusterRoleName() /
// getCollectorClusterRoleName(), which read OPERATOR_ORG and OPERATOR_CHART env vars.
// In Helm deployments the ClusterRoles carry an org:chart: prefix so resourceNames cannot
// be set statically here. 'bind' on clusterroles without resourceNames is intentional and
// safe — 'bind' only allows creating a ClusterRoleBinding that references an existing role;
// it does not grant the permissions that role contains.
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=bind
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=create;get;list;update;delete
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=create;delete;get;list
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=addondeploymentconfigs;clustermanagementaddons;managedclusteraddons,verbs=create;get;list;delete;update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers;clustermanagementaddons/finalizers;managedclusteraddons/finalizers,verbs=update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status;clustermanagementaddons/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=authentication.open-cluster-management.io,resources=managedserviceaccounts,verbs=create;get;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list
//+kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs,verbs=get;list
//+kubebuilder:rbac:groups=proxy.open-cluster-management.io,resources=clusterstatuses/aggregator,verbs=create
//+kubebuilder:rbac:groups=rbac.open-cluster-management.io,resources=clusterpermissions,verbs=create;get;delete
//+kubebuilder:rbac:groups=search.open-cluster-management.io,resources=searches,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=search.open-cluster-management.io,resources=searches/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=search.open-cluster-management.io,resources=searches/finalizers,verbs=update
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=create;delete;get;list;patch
//+kubebuilder:rbac:groups=multicluster.openshift.io,resources=multiclusterengines,verbs=get;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *SearchReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log.V(2).Info("Reconciling from search-v2-operator for ", req.Name, req.Namespace)
	r.context = ctx
	instance := &searchv1alpha1.Search{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: "search-v2-operator", Namespace: req.Namespace}, instance) //nolint:staticcheck // "could remove embedded field 'Client' from selector
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	// Start the addon framework part of search controller.
	// This is in charge of approving CertificateSigningRequest for managed clusters.
	once.Do(func() {
		addon.CreateAddonOnce(ctx, instance)
	})

	// Update status
	if strings.HasPrefix(req.Name, "Pod/") {
		podName := strings.Split(req.Name, "/")[1]
		log.V(2).Info("Received reconcile for pod", "Updating status for pod ", podName)
		err := r.updateStatus(ctx, instance, podName)
		return ctrl.Result{}, err
	}
	// Setup finalizers and permit search deletion irrespective of search-pause annotation - ACM-15203
	deleted, err := r.setFinalizer(ctx, instance)
	if err != nil || deleted {
		if deleted {
			log.V(2).Info("Search Instance deleted, requeue request")
		} else {
			log.V(2).Info("Error setting Finalizer, requeue request ", "error", err.Error())
		}
		return ctrl.Result{}, err
	}
	// Do not reconcile objects if this instance of search is labeled "paused"
	if IsPaused(instance.GetAnnotations()) {
		log.Info("Reconciliation is paused because the annotation 'search-pause: true' was found.")
		return ctrl.Result{}, nil
	}

	if instance.Spec.DBStorage.StorageClassName != "" && !r.isPVCPresent(ctx, instance) {

		pvcConfigured := r.configurePVC(ctx, instance)
		if !pvcConfigured {
			log.Info("Persistent Volume Claim is not ready yet, retrying in 10 seconds")
			return ctrl.Result{
				RequeueAfter: 10 * time.Second,
				Requeue:      true,
			}, nil
		}
	}
	result, err := r.createSearchServiceAccount(ctx, r.SearchAPIServiceAccount(instance))
	if result != nil {
		log.Error(err, "SearchAPIServiceAccount setup failed")
		return *result, err
	}
	postgresSA, err := r.SearchPostgresServiceAccount(instance)
	if err != nil {
		log.Error(err, "SearchPostgresServiceAccount build failed")
		return ctrl.Result{}, err
	}
	result, err = r.createSearchServiceAccount(ctx, postgresSA)
	if result != nil {
		log.Error(err, "SearchPostgresServiceAccount setup failed")
		return *result, err
	}
	indexerSA, err := r.SearchIndexerServiceAccount(instance)
	if err != nil {
		log.Error(err, "SearchIndexerServiceAccount build failed")
		return ctrl.Result{}, err
	}
	result, err = r.createSearchServiceAccount(ctx, indexerSA)
	if result != nil {
		log.Error(err, "SearchIndexerServiceAccount setup failed")
		return *result, err
	}
	result, err = r.createSearchServiceAccount(ctx, r.SearchCollectorServiceAccount(instance))
	if result != nil {
		log.Error(err, "SearchCollectorServiceAccount setup failed")
		return *result, err
	}
	// "search-api" and "search-collector" ClusterRoles are pre-provisioned as static
	// manifests and must exist before the operator starts.  Only their ClusterRoleBindings
	// are reconciled here.
	result, err = r.createUpdateRoles(ctx, r.IndexerClusterRole(instance))
	if result != nil {
		log.Error(err, "IndexerClusterRole setup failed")
		return *result, err
	}
	result, err = r.createUpdateRoles(ctx, r.AddonClusterRole(instance))
	if result != nil {
		log.Error(err, "AddonClusterRole setup failed")
		return *result, err
	}
	result, err = r.createUpdateRoles(ctx, r.GlobalSearchUserClusterRole(instance))
	if result != nil {
		log.Error(err, "GlobalSearchUserClusterRole setup failed")
		return *result, err
	}

	result, err = r.createRoleBinding(ctx, r.APIClusterRoleBinding(instance))
	if result != nil {
		log.Error(err, "APIClusterRoleBinding setup failed")
		return *result, err
	}
	result, err = r.createRoleBinding(ctx, r.CollectorClusterRoleBinding(instance))
	if result != nil {
		log.Error(err, "CollectorClusterRoleBinding setup failed")
		return *result, err
	}
	result, err = r.createRoleBinding(ctx, r.IndexerClusterRoleBinding(instance))
	if result != nil {
		log.Error(err, "IndexerClusterRoleBinding setup failed")
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
	result, err = r.createServiceMonitor(ctx, r.ServiceMonitor(instance, "search-indexer", instance.Namespace))
	if result != nil {
		log.Error(err, "ServiceMonitor setup failed for search-indexer")
		return *result, err
	}
	result, err = r.createServiceMonitor(ctx, r.ServiceMonitor(instance, "search-api", instance.Namespace))
	if result != nil {
		log.Error(err, "ServiceMonitor setup failed for search-api")
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
	result, err = r.createOrUpdateConfigMap(ctx, r.PostgresConfigmap(instance))
	if result != nil {
		log.Error(err, "Postgres configmap setup failed")
		return *result, err
	}
	result, err = r.createConfigMap(ctx, r.SearchCACert(instance))
	if result != nil {
		log.Error(err, "Search CACert setup failed")
		return *result, err
	}

	result, err = r.reconcileGlobalSearch(ctx, instance)
	if err != nil {
		log.Error(err, "Global Search setup failed")
		return *result, err
	}
	result, err = r.addEnvToSearchAPI(ctx, instance)
	if err != nil {
		log.Error(err, "Adding HUB_NAME env to search api deployment failed")
		return *result, err
	}

	result, err = r.reconcileVirtualMachineConfiguration(ctx, instance)
	if err != nil {
		log.Error(err, "Virtual Machine setup failed")
		return *result, err
	}

	cleanOnce.Do(func() {
		// delete legacy servicemonitor setup
		// Starting with ACM 2.9, ServiceMonitors are created in the open-cluster-management namespace.
		// This function removes the service monitors that were previously created in the openshift-monitoring namespace.
		// We can remove this migration step after ACM 2.8 End of Life.
		r.deleteLegacyServiceMonitorSetup(instance)

		// remove Search ownerref from ClusterManagementAddon
		// Starting with ACM 2.10, the ClusterManagementAddon is owned by the mch operator.
		err := r.removeOwnerRefClusterManagementAddon(instance)
		if err != nil {
			log.Error(err, "Failed to remove Search ownerRef from ClusterManagementAddon")
		}

	})

	// Remove legacy shared RBAC objects left on clusters upgraded from pre-ACM-34432 releases.
	// Before ACM-34432 the operator created a shared "search" ClusterRole (carrying impersonate),
	// a matching "search" ClusterRoleBinding, and a "search-serviceaccount" ServiceAccount.
	// These are no longer used — every component now has a dedicated ServiceAccount and the
	// operator SA no longer holds impersonate. Cleaning them up closes the lingering
	// privilege escalation path on upgraded clusters.
	//
	// This runs outside cleanOnce so that transient API errors are retried on the next
	// reconcile rather than permanently skipped. deleteClusterRole/deleteClusterRoleBinding
	// are no-ops when the object is already gone (IsNotFound is swallowed inside them).
	if err := r.deleteClusterRole(instance, getRoleName()); err != nil {
		log.Error(err, "Failed to delete legacy ClusterRole", "name", getRoleName())
		return ctrl.Result{}, err
	}
	if err := r.deleteClusterRoleBinding(instance, getRoleBindingName()); err != nil {
		log.Error(err, "Failed to delete legacy ClusterRoleBinding", "name", getRoleBindingName())
		return ctrl.Result{}, err
	}
	legacySA := &corev1.ServiceAccount{}
	legacySA.Name = "search-serviceaccount"
	legacySA.Namespace = instance.GetNamespace()
	if err := r.Delete(ctx, legacySA); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Failed to delete legacy ServiceAccount", "name", "search-serviceaccount")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SearchReconciler) SetupWithManager(mgr ctrl.Manager) error {
	pred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
	}
	// Trigger on create or update.
	triggerOnUpdatePred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return true
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&searchv1alpha1.Search{}).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(),
			&searchv1alpha1.Search{}, handler.OnlyControllerOwner()), builder.WithPredicates(pred)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(),
			&searchv1alpha1.Search{}, handler.OnlyControllerOwner()), builder.WithPredicates(pred)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(),
			&searchv1alpha1.Search{}, handler.OnlyControllerOwner()), builder.WithPredicates(triggerOnUpdatePred)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, a client.Object) []reconcile.Request {
				// Trigger reconcile if search pod
				if searchLabels(a.GetLabels()) {
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      "Pod/" + a.GetName(),
								Namespace: a.GetNamespace(),
							},
						},
					}
				} else {
					return nil
				}

			}),
		).
		Complete(r)
}

func (r *SearchReconciler) updateStatus(ctx context.Context, instance *searchv1alpha1.Search, podName string) error {
	deploymentName := strings.Join(strings.Split(podName, "-")[:2], "-")
	opts := []client.ListOption{client.MatchingLabels{"app": "search", "name": deploymentName}}
	// fetch the pods
	podList := &corev1.PodList{}
	err := r.Client.List(ctx, podList, opts...) //nolint:staticcheck // "could remove embedded field 'Client' from selector
	if err != nil {
		log.Error(err, "Error listing pods for component", deploymentName)
		return err
	}
	// if no pods are found, output an error message on the status
	if len(podList.Items) == 0 {
		podList.Items = append(podList.Items, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: podName},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.Now(),
						Reason: "NoPodsFound", Message: "Check status of deployment: " + deploymentName}}},
		})
		log.Info("No pods found for deployment ", deploymentName, "listing pods failed")
	}
	updateStatusCondition(instance, podList)
	instance.Status.Storage = instance.Spec.DBStorage.StorageClassName
	instance.Status.DB = DBNAME // This stored in the search-postgres secret, but currently it is a static value

	// write instance with the new values
	err = r.Client.Status().Update(ctx, instance)
	if err != nil {
		if errors.IsConflict(err) {
			log.Error(err, "Failed to update status for Search CR instance: Object has been modified")
		} else {
			log.Error(err, "Failed to update status for Search CR instance")
		}
		return err
	}
	log.Info("Updated Search CR status successfully")

	return nil
}

func (r *SearchReconciler) finalizeSearch(instance *searchv1alpha1.Search) error {
	err := r.removeOwnerRefClusterManagementAddon(instance)
	if err != nil {
		return err
	}
	addonCRName := getAddonRoleName()
	err = r.deleteClusterRole(instance, addonCRName)
	if err != nil {
		return err
	}
	err = r.deleteClusterRoleBinding(instance, addonCRName)
	if err != nil {
		return err
	}
	// "search-api" ClusterRole is pre-provisioned as a static manifest — do NOT delete it.
	// Delete only the ClusterRoleBinding the operator created.
	err = r.deleteClusterRoleBinding(instance, getRoleBindingName()+"-api")
	if err != nil {
		return err
	}
	// "search-collector" ClusterRole is pre-provisioned as a static manifest — do NOT delete it.
	// Delete only the ClusterRoleBinding the operator created.
	err = r.deleteClusterRoleBinding(instance, getRoleBindingName()+"-collector")
	if err != nil {
		return err
	}
	// The indexer ClusterRole and ClusterRoleBinding are cluster-scoped and cannot carry
	// an owner reference (Search is namespaced), so they must be deleted explicitly here.
	err = r.deleteClusterRole(instance, getRoleName()+"-indexer")
	if err != nil {
		return err
	}
	err = r.deleteClusterRoleBinding(instance, getRoleBindingName()+"-indexer")
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
			err := r.Update(ctx, instance)
			if err != nil {
				log.Error(err, "Error updating instance while removing finalizer")
				return false, err
			}
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
			log.V(2).Info("Error updating instance with Finalizer", "error", err.Error())
			return false, err
		}
	}
	return false, nil
}
