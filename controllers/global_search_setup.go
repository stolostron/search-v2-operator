// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	searchGlobal       = "search-global"
	searchGlobalConfig = "search-global-config"
)

var (
	multiclusterGlobalHubGvr = schema.GroupVersionResource{
		Group:    "operator.open-cluster-management.io",
		Version:  "v1alpha4",
		Resource: "multiclusterglobalhubs",
	}
	multiclusterengineResourceGvr = schema.GroupVersionResource{
		Group:    "multicluster.openshift.io",
		Version:  "v1",
		Resource: "multiclusterengines",
	}
	managedClusterResourceGvr = schema.GroupVersionResource{
		Group:    "cluster.open-cluster-management.io",
		Version:  "v1",
		Resource: "managedclusters",
	}
	managedServiceAccountGvr = schema.GroupVersionResource{
		Group:    "authentication.open-cluster-management.io",
		Version:  "v1beta1",
		Resource: "managedserviceaccounts",
	}
	manifestWorkGvr = schema.GroupVersionResource{
		Group:    "work.open-cluster-management.io",
		Version:  "v1",
		Resource: "manifestworks",
	}
)

// Reconcile Global Search.
//  1. Check the global-search-preview annotation.
//  2. Validate pre-requisites.
//     a. MulticlusterGlobalHub operator is installed in the cluster.
//     b. The ManagedServiceAccount add-on is enabled in the MultiClusterEngine CR.
//     c. The ClusterProxy addon is enabled in the MultiClusterEngine CR.
//  3. Enable global search feature in the console.
//  4. Enable federated search feature in the search-api deployment.
//  5. Create a ManagedServiceAccount recource for each Managed Hub.
//  6. Create a ManifestWork resource for each Managed Hub.
func (r *SearchReconciler) reconcileGlobalSearch(ctx context.Context,
	instance *searchv1alpha1.Search) (*reconcile.Result, error) {

	if instance.ObjectMeta.Annotations["search.open-cluster-management.io/global-search-preview"] == "true" ||
		instance.ObjectMeta.Annotations["global-search-preview"] == "true" {

		log.Info("The global-search-preview annotation is present. Setting up global search...")

		// Validate global search pre-requisites.
		err := r.validateGlobalSearchPrerequisites(ctx)
		if err != nil {
			log.Error(err, "Failed to verify global search pre-requisites.")
			return &reconcile.Result{}, err
		}

		// Enable global search.
		err = r.enableGlobalSearch(ctx, instance)
		if err != nil {
			log.Info("Failed to enable global search. Updating CR status conditions.", "error", err.Error())

			updateErr := r.updateGlobalSearchStatus(ctx, instance, metav1.Condition{
				Type:               "GlobalSearchReady",
				Status:             metav1.ConditionFalse,
				Reason:             "GlobalSearchSetupFailed",
				Message:            "Failed to enable global search. " + err.Error(),
				LastTransitionTime: metav1.Now(),
			})
			if updateErr != nil {
				log.Error(updateErr, "Failed to update Global Search status condition on Search CR instance.")
			}

		} else {
			updateErr := r.updateGlobalSearchStatus(ctx, instance, metav1.Condition{
				Type:               "GlobalSearchReady",
				Status:             metav1.ConditionTrue,
				Reason:             "None",
				Message:            "None",
				LastTransitionTime: metav1.Now(),
			})
			if updateErr != nil {
				log.Error(updateErr, "Failed to update the Global Search status condition on Search CR instance.")
			}
		}
	} else {
		log.Info("The global-search-preview annotation is not present. Checking if global search was enabled before.")

		// Use the status conditions to determine if global search was enabled before this reconcile.
		globalSearchPresent := false
		globalSearchConditionIndex := -1
		for condIndex, condition := range instance.Status.Conditions {
			if condition.Type == "GlobalSearchReady" {
				globalSearchPresent = true
				globalSearchConditionIndex = condIndex
				break
			}
		}
		if globalSearchPresent {
			err := r.disableGlobalSearch(ctx, instance)
			if err != nil {
				log.Error(err, "Failed to disable global search.")
				updateErr := r.updateGlobalSearchStatus(ctx, instance, metav1.Condition{
					Type:               "GlobalSearchReady",
					Status:             metav1.ConditionFalse,
					Reason:             "GlobalSearchCleanupFailed",
					Message:            "Failed to disable global search. " + err.Error(),
					LastTransitionTime: metav1.Now(),
				})
				if updateErr != nil {
					log.Error(updateErr, "Failed to update Search CR instance status.")
				}
				return &reconcile.Result{}, err
			}

			// Remove the status condition.
			instance.Status.Conditions = append(instance.Status.Conditions[:globalSearchConditionIndex],
				instance.Status.Conditions[globalSearchConditionIndex+1:]...)

			err = r.commitInstanceState(ctx, instance)
			if err != nil {
				log.Error(err, "Failed to update Search CR instance status.")
				return &reconcile.Result{}, err
			}
		}
	}
	return &reconcile.Result{}, nil
}

// Validate pre-requisites.
//
//	a. MulticlusterGlobalHub operator is installed in the cluster.
//	b. The ManagedServiceAccount add-on is enabled in the MultiClusterEngine CR.
//	c. The ClusterProxy addon is enabled in the MultiClusterEngine CR.
func (r *SearchReconciler) validateGlobalSearchPrerequisites(ctx context.Context) error {
	log.V(5).Info("Checking global search pre-requisites.")
	// Verify that MulticlusterGlobalHub operator is installed.
	// oc get multiclusterglobalhub -A
	multiclusterGlobalHub, err := r.DynamicClient.Resource(multiclusterGlobalHubGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("MulticlusterGlobalHub operator is not installed.")
	} else if len(multiclusterGlobalHub.Items) > 0 {
		log.V(5).Info("Found MulticlusterGlobalHub intance.")
	}

	// Verify that the ManagedServiceAccount and ClusterProxy add-ons are enabled in the MultiClusterEngine CR.
	mce, err := r.DynamicClient.Resource(multiclusterengineResourceGvr).
		Get(ctx, "multiclusterengine", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Failed to get MulticlusterEngine CR.")
		return fmt.Errorf("Failed to get MulticlusterEngine CR.")
	} else {
		log.V(5).Info("Checking that managedserviceaccount and cluster-proxy-addon are enabled in MCE.")
		managedServiceAccountEnabled := false
		clusterProxyEnabled := false
		components := mce.Object["spec"].(map[string]interface{})["overrides"].(map[string]interface{})["components"]
		for _, component := range components.([]interface{}) {
			if component.(map[string]interface{})["name"] == "managedserviceaccount" &&
				component.(map[string]interface{})["enabled"] == true {
				log.V(5).Info("Managed Service Account add-on is enabled.")
				managedServiceAccountEnabled = true
			}
			if component.(map[string]interface{})["name"] == "cluster-proxy-addon" &&
				component.(map[string]interface{})["enabled"] == true {
				log.V(5).Info("Cluster Proxy add-on is enabled.")
				clusterProxyEnabled = true
			}
		}
		if !managedServiceAccountEnabled {
			return fmt.Errorf("Managed Service Account add-on is not enabled in MulticlusterEngine.")
		}
		if !clusterProxyEnabled {
			return fmt.Errorf("Cluster Proxy add-on is not enabled in MulticlusterEngine.")
		}
	}
	return nil
}

// Logic to enable Global Search.
//  1. Enable global search feature in the console.
//     a. Add globalSearchFeatureFlag=true to configmap console-mce-config in multicluster-engine namespace.
//     b. Add globalSearchFeatureFlag=true to configmap console-config in open-cluster-management namespace.
//  2. Enable federated search feature in the search-api deployment.
//  3. Create configuration resources for each Managed Hub.
//     a. Create a ManagedServiceAccount search-global.
//     b. Create a ManifestWork search-global-config if it doesn't exist.
func (r *SearchReconciler) enableGlobalSearch(ctx context.Context, instance *searchv1alpha1.Search) error {
	// 1. Enable global search feature in the console.
	// 1a.Add globalSearchFeatureFlag=true to configmap console-mce-config in multicluster-engine namespace.
	err := r.updateConsoleConfig(ctx, true, "multicluster-engine", "console-mce-config")
	if err != nil {
		log.Error(err, "Failed to enable the global search feature in console-mce-config.")
		return err // QUESTION: Should we return here or continue? Should we rollback other changes?
	}
	// 1b. Add globalSearchFeatureFlag=true to configmap console-config in open-cluster-management namespace.
	err = r.updateConsoleConfig(ctx, true, instance.GetNamespace(), "console-config")
	if err != nil {
		log.Error(err, "Failed to enable the global search feature in console-config.")
		return err // QUESTION: Should we return here or continue? Should we rollback other changes?
	}

	// 2. Enable federated search feature in the search-api deployment.
	err = r.updateSearchApiDeployment(ctx, true, instance)
	if err != nil {
		log.Error(err, "Failed to enable the federated global search feature.")
		return err // QUESTION: Should we return here or continue? Should we rollback other changes?
	}

	// Create configuration resources for each Managed Hub.
	// MANAGED_HUBS=($(oc get managedcluster -o json | jq -r '.items[] | select(.status.clusterClaims[] |
	//     .name == "hub.open-cluster-management.io" and .value != "NotInstalled") | .metadata.name'))
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error(err, "Failed to list of ManagedClusters to configure global search.")
		return err // QUESTION: Should we return here or continue? Should we rollback other changes?
	}

	for _, cluster := range clusterList.Items {
		isManagedHub := false
		if cluster.Object["status"] == nil || cluster.Object["status"].(map[string]interface{})["clusterClaims"] == nil {
			log.V(5).Info("Cluster doesn't have status or clusterClaims.", "cluster", cluster.GetName())
			continue
		}
		clusterClaims := cluster.Object["status"].(map[string]interface{})["clusterClaims"].([]interface{})
		for _, claim := range clusterClaims {
			claimMap := claim.(map[string]interface{})
			if claimMap["name"] == "hub.open-cluster-management.io" && claimMap["value"] != "NotInstalled" {
				isManagedHub = true
				break
			}
		}
		if !isManagedHub {
			log.V(5).Info("Cluster is not a Managed Hub.", "name", cluster.GetName())
			continue
		}

		log.V(2).Info("Cluster is a Managed Hub. Configuring global search resources.", "name", cluster.GetName())

		// 3a. Create a ManagedServiceAccount search-global.
		_, err := r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).
			Get(ctx, searchGlobal, metav1.GetOptions{})
		if err != nil && errors.IsNotFound(err) {
			log.V(1).Info("Creating ManagedServiceAccount search-global for managed hub", "name", cluster.GetName())
			managedSA := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "authentication.open-cluster-management.io/v1beta1",
					"kind":       "ManagedServiceAccount",
					"metadata": map[string]interface{}{
						"name": searchGlobal,
						"labels": map[string]interface{}{
							"app": "search",
						},
					},
					"spec": map[string]interface{}{
						"rotation": map[string]interface{}{},
					},
				},
			}
			_, err := r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).
				Create(ctx, managedSA, metav1.CreateOptions{})
			if err != nil {
				log.Error(err, "Failed to create ManagedServiceAccount search-global.")
				// QUESTION: How should we handle partial errors? Should we continue or rollback?
			}
		} else {
			log.V(5).Info("Found ManagedServiceAccount search-global for Managed Hub.", "name", cluster.GetName())
		}

		// 3b. Create a ManifestWork search-global-config if it doesn't exist.
		err = r.createManifestWork(ctx, cluster.GetName())
		if err != nil {
			log.Error(err, "Failed to create ManifestWork search-global-config.")
			// QUESTION: How should we handle partial errors? Should we continue or rollback?
		}
	}
	log.Info("Global search resources configured.")
	return err
}

// Create a ManifestWork search-global-config if it doesn't exist.
func (r *SearchReconciler) createManifestWork(ctx context.Context, cluster string) error {
	_, err := r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster).
		Get(ctx, searchGlobalConfig, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		log.V(1).Info("CreatingManifestWork search-global-config for Managed Hub", "name", cluster)

		manifestWork := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "work.open-cluster-management.io/v1",
				"kind":       "ManifestWork",
				"metadata": map[string]interface{}{
					"name": searchGlobalConfig,
					"labels": map[string]interface{}{
						"app": "search",
					},
				},
				"spec": map[string]interface{}{
					"workload": map[string]interface{}{
						"manifests": []map[string]interface{}{
							{
								"apiVersion": "rbac.authorization.k8s.io/v1",
								"kind":       "ClusterRoleBinding",
								"metadata": map[string]interface{}{
									"name": "search-global-binding",
									"labels": map[string]interface{}{
										"app": "search",
									},
								},
								"roleRef": map[string]interface{}{
									"apiGroup": "rbac.authorization.k8s.io",
									"kind":     "ClusterRole",
									"name":     "global-search-user",
								},
								"subjects": []map[string]interface{}{
									{
										"kind":      "ServiceAccount",
										"name":      searchGlobal,
										"namespace": "open-cluster-management-agent-addon",
									},
								},
							},
							{ // TODO: Remove Route recource and use cluster-proxy-addon instead.
								"apiVersion": "route.openshift.io/v1",
								"kind":       "Route",
								"metadata": map[string]interface{}{
									"name":      "search-global-hub",
									"namespace": "open-cluster-management",
									"labels": map[string]interface{}{
										"app": "search",
									},
								},
								"spec": map[string]interface{}{
									"port": map[string]interface{}{
										"targetPort": "search-api",
									},
									"tls": map[string]interface{}{
										"termination": "passthrough",
									},
									"to": map[string]interface{}{
										"kind":   "Service",
										"name":   "search-search-api",
										"weight": 100,
									},
								},
							},
						},
					},
				},
			},
		}
		_, err := r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster).
			Create(ctx, manifestWork, metav1.CreateOptions{})
		return err
	}
	log.V(5).Info("Found existing ManifestWork search-global-config.")
	return nil
}

// Logic to disable Global Search.
//  1. Disable global search feature in the console.
//  2. Disable federated search feature in the search-api deployment.
//  3. Delete configuration resources for each Managed Hub.
func (r *SearchReconciler) disableGlobalSearch(ctx context.Context, instance *searchv1alpha1.Search) error {
	// 1. Disable global search feature in the console.
	// 1a. Remove the globalSearchFeatureFlag key to configmap console-mce-config in multicluster-engine namespace.
	err := r.updateConsoleConfig(ctx, false, "multicluster-engine", "console-mce-config")
	if err != nil {
		log.Error(err, "Failed to remove the globalSearchFeatureFlag in configmap console-mce-config.")
	}

	// 1b. Remove the globalSearchFeatureFlag key to configmap console-config in open-cluster-management namespace.
	err = r.updateConsoleConfig(ctx, false, instance.GetNamespace(), "console-config")
	if err != nil {
		log.Error(err, "Failed to remove the globalSearchFeatureFlag in configmap console-config.")
	}

	// 2. Disable federated search feature in the search-api.
	// oc patch search search-v2-operator -n open-cluster-management --type='merge'
	//    -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"false"}]}}}}'
	err = r.updateSearchApiDeployment(ctx, false, instance)
	if err != nil {
		log.Error(err, "Failed to remove the federated global search feature flag from search-api deployment.")
	}

	// 3. Delete configuration resources for each Managed Hub.
	// MANAGED_HUBS=($(oc get managedcluster -o json | jq -r '.items[] | select(.status.clusterClaims[] |
	//    .name == "hub.open-cluster-management.io" and .value != "NotInstalled") | .metadata.name'))
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error(err, "Failed to list ManagedClusters.")
	}
	for _, cluster := range clusterList.Items {
		// 3a. Delete the ManagedServiceAccount search-global.
		managedServiceAccount, err := r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).
			Get(ctx, searchGlobal, metav1.GetOptions{})
		if err == nil && managedServiceAccount != nil {
			err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).
				Delete(ctx, searchGlobal, metav1.DeleteOptions{})
			if err != nil {
				log.Error(err, "Failed to delete ManagedServiceAccount search-global on Managed Hub.",
					"name", cluster.GetName())
			}
		}
		// 3b. Delete the ManifestWork search-global-config.
		manifestWork, err := r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).
			Get(ctx, searchGlobalConfig, metav1.GetOptions{})
		if err == nil && manifestWork != nil {
			err = r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).
				Delete(ctx, searchGlobalConfig, metav1.DeleteOptions{})
			if err != nil {
				log.Error(err, "Failed to delete ManifestWork search-global-config on Managed Hub.",
					"name", cluster.GetName())
			}
		}
	}
	log.V(1).Info("Done deleting global search configuration resources.")
	return err
}

// Update flag globalSearchFeatureFlag in console config.
// oc patch configmap {name} -n {namespace} -p '{"data": {"globalSearchFeatureFlag": "enabled"}}'
func (r *SearchReconciler) updateConsoleConfig(ctx context.Context, enabled bool, namespace, name string) error {
	consoleConfig, err := r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).
		Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Error getting console configmap", "name", name, "namespace", namespace)
		return fmt.Errorf("Error getting configmap %s in namespace %s", name, namespace)
	}
	existingValue := consoleConfig.Object["data"].(map[string]interface{})["globalSearchFeatureFlag"]

	// Update configmap if needed.
	if enabled && existingValue != "enabled" {
		log.V(5).Info("Adding globalSearchFeatureFlag to configmap ", "namespace", namespace, "name", name)
		consoleConfig.Object["data"].(map[string]interface{})["globalSearchFeatureFlag"] = "enabled"
	} else if !enabled && existingValue == "enabled" {
		log.V(5).Info("Removing globalSearchFeatureFlag from configmap", "namespace", namespace, "name", name)
		delete(consoleConfig.Object["data"].(map[string]interface{}), "globalSearchFeatureFlag")
	} else {
		log.V(5).Info("globalSearchFeatureFlag already set to ", existingValue)
		return nil
	}

	// Write the updated configmap.
	_, err = r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).Namespace(namespace).
		Update(ctx, consoleConfig, metav1.UpdateOptions{})
	return err
}

// Configure the federated global search feature in the search-api deployment.
func (r *SearchReconciler) updateSearchApiDeployment(ctx context.Context, enabled bool,
	instance *searchv1alpha1.Search) error {
	changed := false

	if enabled {
		// oc patch search search-v2-operator -n open-cluster-management --type='merge'
		// -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"true"}]}}}}'
		if instance.Spec.Deployments.QueryAPI.Env == nil {
			instance.Spec.Deployments.QueryAPI.Env = append(instance.Spec.Deployments.QueryAPI.Env,
				corev1.EnvVar{Name: "FEATURE_FEDERATED_SEARCH", Value: "true"})
			changed = true
		} else {
			for _, env := range instance.Spec.Deployments.QueryAPI.Env {
				if env.Name == "FEATURE_FEDERATED_SEARCH" && env.Value != "true" {
					env.Value = "true"
					changed = true
					break
				}
			}
		}
	} else {
		// oc patch search search-v2-operator -n open-cluster-management --type='merge'
		//   -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"false"}]}}}}'
		if instance.Spec.Deployments.QueryAPI.Env != nil {
			for i, env := range instance.Spec.Deployments.QueryAPI.Env {
				if env.Name == "FEATURE_FEDERATED_SEARCH" {
					instance.Spec.Deployments.QueryAPI.Env = append(instance.Spec.Deployments.QueryAPI.Env[:i],
						instance.Spec.Deployments.QueryAPI.Env[i+1:]...)
					changed = true
					break
				}
			}
		}
	}
	if changed {
		err := r.commitInstanceState(ctx, instance)
		if err != nil {
			log.Error(err, "Failed to update Search API env in the Search instance.")
		}
		return err
	}
	return nil
}

func (r *SearchReconciler) updateGlobalSearchStatus(ctx context.Context, instance *searchv1alpha1.Search,
	status metav1.Condition) error {
	// Find existing status condition.
	existingConditionIndex := -1
	for i, condition := range instance.Status.Conditions {
		if condition.Type == "GlobalSearchReady" {
			existingConditionIndex = i
			break
		}
	}
	existingConditions := instance.Status.Conditions
	if existingConditionIndex == -1 {
		// Add new condition.
		instance.Status.Conditions = append(instance.Status.Conditions, status)
	} else if existingConditions[existingConditionIndex].Status != status.Status ||
		existingConditions[existingConditionIndex].Reason != status.Reason ||
		existingConditions[existingConditionIndex].Message != status.Message {
		// Update existing condition, only if anything changed.
		instance.Status.Conditions[existingConditionIndex] = status
	} else {
		// Nothing has changed.
		log.Info("Global search status condition did not change.")
		return nil
	}

	// write instance with the new status.
	err := r.commitInstanceState(ctx, instance)

	log.Info("Updated global search status in search CR successfully.")
	return err
}

// Write instance with the new state values.
func (r *SearchReconciler) commitInstanceState(ctx context.Context, instance *searchv1alpha1.Search) error {
	err := r.Client.Status().Update(ctx, instance)
	if err != nil {
		if errors.IsConflict(err) {
			log.Error(err, "Failed to update status for Search CR instance: Object has been modified.")
		}
		log.Error(err, "Failed to update status for Search CR instance.")
	}
	return err
}
