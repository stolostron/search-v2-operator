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
	managedClusterAddonResourceGvr = schema.GroupVersionResource{
		Group:    "addon.open-cluster-management.io",
		Version:  "v1alpha1",
		Resource: "managedclusteraddons",
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

// Logic to enable Global Search.
func (r *SearchReconciler) enableGlobalSearch(ctx context.Context, instance *searchv1alpha1.Search) error {
	log.Info("Reconcile Global Search resources.")

	// Verify that MulticlusterGlobalHub operator is installed.
	// oc get multiclusterglobalhub -A
	multiclusterGlobalHub, err := r.DynamicClient.Resource(multiclusterGlobalHubGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("MulticlusterGlobalHub operator is not installed.")
	} else if len(multiclusterGlobalHub.Items) > 0 {
		log.Info("Found MulticlusterGlobalHub intance.")
	}

	// Enable global search feature in the console.
	// Adds the globalSearchFeatureFlag key to ConfigMap console-mce-config in multicluster-engine namespace.
	err = r.updateConsoleConfig(ctx, true, "console-mce-config", "multicluster-engine")
	if err != nil {
		log.Error(err, "Failed to enable the Global Search feature in the console. multicluster-engine.")
	}

	// Adds the globalSearchFeatureFlag key to ConfigMap console-config in open-cluster-management namespace.
	err = r.updateConsoleConfig(ctx, true, "console-config", instance.GetNamespace())
	if err != nil {
		log.Error(err, "Failed to enable the Global Search feature in the console.")
	}

	// Enable federated search feature in the search-api.
	err = r.configureFederatedGlobalSearchFeature(ctx, true, instance)
	if err != nil {
		log.Error(err, "Failed to enable the federated global search feature.")
	}

	// Enable the Managed Service Account add-on in the MultiClusterEngine CR.
	// oc patch multiclusterengine multiclusterengine --type='json' -p='[{"op": "add", "path": "/spec/overrides/components", "value": [{"name": "managedserviceaccount", "enabled": true}]}]'
	mce, err := r.DynamicClient.Resource(multiclusterengineResourceGvr).Get(ctx, "multiclusterengine", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Failed to get MulticlusterEngine CR.")
	} else {
		log.Info("Found MulticlusterEngine. Checking that ManagedServiceAccount is enabled.")
		managedServiceAccountEnabled := false

		// Check if the Managed Service Account add-on is enabled.
		components := mce.Object["spec"].(map[string]interface{})["overrides"].(map[string]interface{})["components"].([]interface{})
		for _, component := range components {
			if component.(map[string]interface{})["name"] == "managedserviceaccount" {
				log.Info("Managed Service Account add-on is enabled.")
				managedServiceAccountEnabled = true
				break
			}
		}
		if !managedServiceAccountEnabled {
			log.Info("Error: Managed Service Account add-on is not enabled.")
		}
	}

	// Create configuration resources for each Managed Hub.
	// MANAGED_HUBS=($(oc get managedcluster -o json | jq -r '.items[] | select(.status.clusterClaims[] | .name == "hub.open-cluster-management.io" and .value != "NotInstalled") | .metadata.name'))
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error(err, "Failed to list ManagedClusters.")
	}

	for _, cluster := range clusterList.Items {
		isManagedHub := false
		if cluster.Object["status"] == nil || cluster.Object["status"].(map[string]interface{})["clusterClaims"] == nil {
			log.Info("Cluster doesn't have status or clusterClaims.", "cluster", cluster.GetName())
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
			log.Info("> Cluster is not a Managed Hub. Skipping.", "name", cluster.GetName())
			continue
		} else {
			log.Info("> Cluster is a Managed Hub. Configuring...", "name", cluster.GetName())
			// 0. FUTURE: Validate that the Managed Hub is running ACM 2.9.0 or later.

			// 1. Verify the ManagedClusterAddon managed-serviceaccount exists.
			_, err := r.DynamicClient.Resource(managedClusterAddonResourceGvr).Namespace(cluster.GetName()).Get(ctx, "managed-serviceaccount", metav1.GetOptions{})
			if err != nil {
				log.Info("Error: ManagedClusterAddon managed-serviceaccount doesn't exist.")
			} else {
				log.Info("Found ManagedClusterAddon managed-serviceaccount.")
			}

			// 2. Create a ManagedServiceAccount
			_, err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).Get(ctx, "search-global", metav1.GetOptions{})
			if err != nil {
				log.Info("Creating ManagedServiceAccount search-global for Managed Hub", "name", cluster.GetName())

				// Create the ManagedServiceAccount search-global.
				managedSA := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "authentication.open-cluster-management.io/v1beta1",
					"kind":       "ManagedServiceAccount",
					"metadata": map[string]interface{}{
						"name": "search-global",
						"labels": map[string]interface{}{
							"app": "search",
						},
					},
					"spec": map[string]interface{}{
						"rotation": map[string]interface{}{},
					},
				},
				}
				_, err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).Create(ctx, managedSA, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create ManagedServiceAccount search-global.")
				}

			} else {
				log.Info("Found ManagedServiceAccount search-global.")
			}

			// 3. Create a ManifestWork
			_, err = r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).Get(ctx, "search-global-config", metav1.GetOptions{})
			if err != nil {
				log.Info("CreatingManifestWork search-global-config for Managed Hub", "name", cluster.GetName())

				// Create the ManifestWork search-global-config.
				manifestWork := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "work.open-cluster-management.io/v1",
					"kind":       "ManifestWork",
					"metadata": map[string]interface{}{
						"name": "search-global-config",
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
											"name":      "search-global",
											"namespace": "open-cluster-management-agent-addon",
										},
									},
								},
								{
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
				_, err = r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).Create(ctx, manifestWork, metav1.CreateOptions{})
				if err != nil {
					log.Error(err, "Failed to create ManifestWork search-global-config.")
				}
			} else {
				log.Info("Found ManifestWork search-global-config.")
			}
		}
	}

	log.Info("Done reconciling Global Search resources.")
	return nil
}

// Logic to disable Global Search.
func (r *SearchReconciler) disableGlobalSearch(ctx context.Context, instance *searchv1alpha1.Search) error {
	log.Info("Reconcile Global Search resources.")

	// Disable global search feature in the console.
	// err := r.configureGlobalSearchConsole(ctx, false, instance)
	// if err != nil {
	// 	log.Error(err, "Failed to disable Global Search feature in the console.")
	// }

	// Disable global search feature in the console.
	// Remove the globalSearchFeatureFlag key to ConfigMap console-mce-config in multicluster-engine namespace.
	err := r.updateConsoleConfig(ctx, false, "console-mce-config", "multicluster-engine")
	if err != nil {
		log.Error(err, "Failed to enable the Global Search feature in the console. multicluster-engine.")
	}

	// Remove the globalSearchFeatureFlag key to ConfigMap console-config in open-cluster-management namespace.
	err = r.updateConsoleConfig(ctx, false, "console-config", instance.GetNamespace())
	if err != nil {
		log.Error(err, "Failed to enable the Global Search feature in the console.")
	}

	// Disable federated search feature in the search-api.
	// oc patch search search-v2-operator -n open-cluster-management --type='merge' -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"false"}]}}}}'
	err = r.configureFederatedGlobalSearchFeature(ctx, false, instance)
	if err != nil {
		log.Error(err, "Failed to disable the federated global search feature.")
	}

	// Delete configuration resources for each Managed Hub.
	// MANAGED_HUBS=($(oc get managedcluster -o json | jq -r '.items[] | select(.status.clusterClaims[] | .name == "hub.open-cluster-management.io" and .value != "NotInstalled") | .metadata.name'))
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error(err, "Failed to list ManagedClusters.")
	}
	for _, cluster := range clusterList.Items {
		// Delete the ManagedServiceAccount search-global.
		managedServiceAccount, err := r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).Get(ctx, "search-global", metav1.GetOptions{})
		if err == nil && managedServiceAccount != nil {
			log.Info(("Deleting ManagedServiceAccount search-global for Managed Hub"), "name", cluster.GetName())
			err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).Delete(ctx, "search-global", metav1.DeleteOptions{})
			if err != nil {
				log.Error(err, "Failed to delete ManagedServiceAccount search-global.")
			}
		}

		// Delete the ManifestWork search-global-config.
		manifestWork, err := r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).Get(ctx, "search-global-config", metav1.GetOptions{})
		if err == nil && manifestWork != nil {
			log.Info("Deleting ManifestWork search-global-config for Managed Hub", "name", cluster.GetName())
			err = r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).Delete(ctx, "search-global-config", metav1.DeleteOptions{})
			if err != nil {
				log.Error(err, "Failed to delete ManifestWork search-global-config.")
			}
		}

	}

	log.Info("Done deleting Global Search configuration resources.")

	return err
}

// Update flag globalSearchFeatureFlag in console config.
func (r *SearchReconciler) updateConsoleConfig(ctx context.Context, enabled bool, name, namespace string) error {
	// oc patch configmap {name} -n {namespace} -p '{"data": {"globalSearchFeatureFlag": "enabled"}}'
	consoleConfig, err := r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Failed to get ConfigMap console-config in ", "namespace", namespace)
	} else {
		if enabled {
			log.Info("Adding globalSearchFeatureFlag to configmap ", "namespace", namespace, "name", name)
			consoleConfig.Object["data"].(map[string]interface{})["globalSearchFeatureFlag"] = "enabled"
		} else {
			log.Info("Removing globalSearchFeatureFlag from configmap", "namespace", namespace, "name", name)
			delete(consoleConfig.Object["data"].(map[string]interface{}), "globalSearchFeatureFlag")
		}

		_, err = r.DynamicClient.Resource(corev1.SchemeGroupVersion.WithResource("configmaps")).Namespace(namespace).Update(ctx, consoleConfig, metav1.UpdateOptions{})
		if err != nil {
			log.Error(err, "Failed to update configmap console-mce-config in multicluster-engine.")
		}
	}
	return err
}

// Configure the federated global search feature in the search-api.
func (r *SearchReconciler) configureFederatedGlobalSearchFeature(ctx context.Context, enabled bool, instance *searchv1alpha1.Search) error {
	if enabled {
		// Enable federated search feature in the search-api.
		// oc patch search search-v2-operator -n open-cluster-management --type='merge' -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"true"}]}}}}'
		if instance.Spec.Deployments.QueryAPI.Env == nil { // TODO: OR if FEATURE_FEDERATED_SEARCH is not already set to true
			instance.Spec.Deployments.QueryAPI.Env = append(instance.Spec.Deployments.QueryAPI.Env, corev1.EnvVar{Name: "FEATURE_FEDERATED_SEARCH", Value: "true"})
			err := r.Client.Update(ctx, instance)
			if err != nil {
				log.Error(err, "Failed to update Search instance.")
			}
		}
	} else {
		// Disable federated search feature in the search-api.
		// oc patch search search-v2-operator -n open-cluster-management --type='merge' -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"false"}]}}}}'
		if instance.Spec.Deployments.QueryAPI.Env != nil {
			for i, env := range instance.Spec.Deployments.QueryAPI.Env {
				if env.Name == "FEATURE_FEDERATED_SEARCH" {
					log.Info("Removing FEATURE_FEDERATED_SEARCH.")
					instance.Spec.Deployments.QueryAPI.Env = append(instance.Spec.Deployments.QueryAPI.Env[:i], instance.Spec.Deployments.QueryAPI.Env[i+1:]...)
					err := r.Client.Update(ctx, instance)
					if err != nil {
						log.Error(err, "Failed to update Search instance.")
					}
					break
				}
			}
		}
	}

	return nil // TODO: Return error if any
}

func (r *SearchReconciler) updateGlobalSearchStatus(ctx context.Context, instance *searchv1alpha1.Search, status metav1.Condition) error {
	// Find existing status condition.
	existingConditionIndex := -1
	for i, condition := range instance.Status.Conditions {
		if condition.Type == "GlobalSearchReady" {
			existingConditionIndex = i
			break
		}
	}
	if existingConditionIndex == -1 {
		// Add new condition.
		instance.Status.Conditions = append(instance.Status.Conditions, status)
	} else {
		// Update existing condition.
		instance.Status.Conditions[existingConditionIndex] = status
	}

	// write instance with the new status values.
	// FIXME: combine with other status update to avoid multiple writes.
	err := r.Client.Status().Update(ctx, instance)
	if err != nil {
		if errors.IsConflict(err) {
			log.Error(err, "Failed to update status for Search CR instance: Object has been modified")
		}
		log.Error(err, "Failed to update status for Search CR instance")
		return err
	}
	log.Info("Updated Search CR status successfully")

	return nil
}
