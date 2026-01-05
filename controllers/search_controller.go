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
	"os"
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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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
//+kubebuilder:rbac:groups="",resources=groups;secrets;serviceaccounts;services;users,verbs=create;get;list;watch;patch;update;delete;impersonate
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=create;get;list;update;delete
//+kubebuilder:rbac:groups=authentication.k8s.io;authorization.k8s.io,resources=uids;userextras/authentication.kubernetes.io/credential-id;userextras/authentication.kubernetes.io/node-name;userextras/authentication.kubernetes.io/node-uid;userextras/authentication.kubernetes.io/pod-uid;userextras/authentication.kubernetes.io/pod-name,verbs=impersonate
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update;patch;watch
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=create;delete;get;list
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=create;get;update
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=get;create
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=certificatesigningrequests;certificatesigningrequests/approval,verbs=get;list;watch;create;update
//+kubebuilder:rbac:groups=certificates.k8s.io,resources=signers,verbs=approve
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=addondeploymentconfigs;clustermanagementaddons;managedclusteraddons,verbs=create;get;list;delete;update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/finalizers;clustermanagementaddons/finalizers;managedclusteraddons/finalizers,verbs=update
//+kubebuilder:rbac:groups=addon.open-cluster-management.io,resources=managedclusteraddons/status;clustermanagementaddons/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=authentication.open-cluster-management.io,resources=managedserviceaccounts,verbs=create;get;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=operator.open-cluster-management.io,resources=multiclusterglobalhubs;multiclusterhubs,verbs=get;list
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
	result, err := r.createSearchServiceAccount(ctx, r.SearchServiceAccount(instance))
	if result != nil {
		log.Error(err, "SearchServiceAccount setup failed")
		return *result, err
	}
	result, err = r.createUpdateRoles(ctx, r.ClusterRole(instance))
	if result != nil {
		log.Error(err, "ClusterRole setup failed")
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
	result, err = r.createRoleBinding(ctx, r.ClusterRoleBinding(instance))
	if result != nil {
		log.Error(err, "ClusterRoleBinding setup failed")
		return *result, err
	}
	result, err = r.createSecret(ctx, r.PGSecret(instance))
	if result != nil {
		log.Error(err, "Postgres Secret setup failed")
		return *result, err
	}
	result, err = r.createService(ctx, r.PGService(instance))
	if result != nil {
		log.Error(err, "Postgres Service setup failed")
		return *result, err
	}
	result, err = r.createOrUpdateDeployment(ctx, r.PGDeployment(instance))
	if result != nil {
		log.Error(err, "Postgres Deployment setup failed")
		return *result, err
	}

	result, err = r.createService(ctx, r.IndexerService(instance))
	if result != nil {
		log.Error(err, "Indexer Service setup failed")
		return *result, err
	}
	result, err = r.createService(ctx, r.APIService(instance))
	if result != nil {
		log.Error(err, "API Service setup failed")
		return *result, err
	}
	result, err = r.createService(ctx, r.CollectorService(instance))
	if result != nil {
		log.Error(err, "Collector Service setup failed")
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
	result, err = r.createServiceMonitor(ctx, r.CollectorServiceMonitor(instance, "search-collector", instance.Namespace))
	if result != nil {
		log.Error(err, "ServiceMonitor setup failed for search-collector")
		return *result, err
	}

	result, err = r.createOrUpdateDeployment(ctx, r.CollectorDeployment(ctx, instance))
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

	result, err = r.reconcileFineGrainedRBACConfiguration(ctx, instance)
	if err != nil {
		log.Error(err, "Fine-grained RBAC setup failed")
		return *result, err
	}

	result, err = r.reconcileVirtualMachineConfiguration(ctx, instance)
	if err != nil {
		log.Error(err, "Virtual Machine setup failed")
		return *result, err
	}

	result, err = r.createOrUpdatePrometheusRule(ctx, r.SearchPVCPrometheusRule(instance))
	if result != nil {
		log.Error(err, "Search PVC prometheus rule setup failed")
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
	// Trigger on create and update for ConfigMaps
	configMapPred := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&searchv1alpha1.Search{}).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(),
			&searchv1alpha1.Search{}, handler.OnlyControllerOwner()), builder.WithPredicates(pred)).
		Watches(&corev1.Secret{}, handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(),
			&searchv1alpha1.Search{}, handler.OnlyControllerOwner()), builder.WithPredicates(pred)).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, a client.Object) []reconcile.Request {
				// Trigger reconcile if SEARCH_GLOBAL_CONFIG configmap
				if a.GetName() == SEARCH_GLOBAL_CONFIG && a.GetNamespace() == os.Getenv("POD_NAMESPACE") {
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      "search-v2-operator",
								Namespace: a.GetNamespace(),
							},
						},
					}
				}
				// Trigger reconcile for owned ConfigMaps
				if metav1.IsControlledBy(a, &searchv1alpha1.Search{}) {
					for _, ref := range a.GetOwnerReferences() {
						if ref.Controller != nil && *ref.Controller {
							return []reconcile.Request{
								{
									NamespacedName: types.NamespacedName{
										Name:      ref.Name,
										Namespace: a.GetNamespace(),
									},
								},
							}
						}
					}
				}
				return nil
			}), builder.WithPredicates(configMapPred)).
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
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, a client.Object) []reconcile.Request {
				if a.GetName() == getRoleName() {
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      "ClusterRole/" + a.GetName(),
								Namespace: os.Getenv("WATCH_NAMESPACE"),
							},
						},
					}
				}
				return nil
			}),
		).
		Watches(&clusterv1.ManagedCluster{}, handler.EnqueueRequestsFromMapFunc(
			func(ctx context.Context, a client.Object) []reconcile.Request {
				// Only trigger reconcile for managedHub clusters
				mc, ok := a.(*clusterv1.ManagedCluster)
				if !ok {
					return nil
				}
				if isManagedHub(mc) {
					return []reconcile.Request{
						{
							NamespacedName: types.NamespacedName{
								Name:      "search-v2-operator",
								Namespace: os.Getenv("POD_NAMESPACE"),
							},
						},
					}
				}
				return nil
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
	searchCRName := getRoleName()
	err = r.deleteClusterRole(instance, searchCRName)
	if err != nil {
		return err
	}
	searchCRBName := getRoleBindingName()
	err = r.deleteClusterRoleBinding(instance, searchCRBName)
	if err != nil {
		return err
	}
	log.Info("Successfully finalized search")
	return nil
}

// isManagedHub checks if a ManagedCluster is a managedHub by inspecting its clusterClaims
func isManagedHub(mc *clusterv1.ManagedCluster) bool {
	if mc == nil {
		return false
	}
	for _, claim := range mc.Status.ClusterClaims {
		if claim.Name == "hub.open-cluster-management.io" && claim.Value != "NotInstalled" {
			return true
		}
	}
	return false
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
