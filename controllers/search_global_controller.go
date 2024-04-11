// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// Logic to enable Global Search.
func (r *SearchReconciler) enableGlobalSearch(ctx context.Context, instance *searchv1alpha1.Search) error {
	log.Info("Enabling Global Search.")

	// TODO: Check that MulticlusterGlobalHub operator is installed.

	// Enable global search feature in the console.
	// oc patch configmap console-mce-config -n multicluster-engine -p '{"data": {"globalSearchFeatureFlag": "enabled"}}'
	consoleMceConfig := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: "console-mce-config", Namespace: "multicluster-engine"}, consoleMceConfig)
	if err != nil {
		log.Error(err, "Failed to get ConfigMap console-mce-config.")
	} else {
		consoleMceConfig.Data["globalSearchFeatureFlag"] = "enabled"
		err = r.Client.Update(ctx, consoleMceConfig)
		if err != nil {
			log.Error(err, "Failed to update ConfigMap console-mce-config.")
		}
	}

	// oc patch configmap console-config -n open-cluster-management -p '{"data": {"globalSearchFeatureFlag": "enabled"}}'
	consoleConfig := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: "console-config", Namespace: instance.GetNamespace()}, consoleConfig)
	if err != nil {
		log.Error(err, "Failed to get ConfigMap console-config.")
	} else {
		consoleConfig.Data["globalSearchFeatureFlag"] = "enabled"
		err = r.Client.Update(ctx, consoleConfig)
		if err != nil {
			log.Error(err, "Failed to update ConfigMap console-config.")
		}
	}

	// # Enable federated search feature in the search-api.
	// oc patch search search-v2-operator -n open-cluster-management --type='merge' -p '{"spec":{"deployments":{"queryapi":{"envVar":[{"name":"FEATURE_FEDERATED_SEARCH", "value":"true"}]}}}}'
	if instance.Spec.Deployments.QueryAPI.Env == nil { // TODO: OR if FEATURE_FEDERATED_SEARCH is not already set to true
		instance.Spec.Deployments.QueryAPI.Env = append(instance.Spec.Deployments.QueryAPI.Env, corev1.EnvVar{Name: "FEATURE_FEDERATED_SEARCH", Value: "true"})
		err = r.Client.Update(ctx, instance)
		if err != nil {
			log.Error(err, "Failed to update Search instance.")
		}
	}

	// Enable the Managed Service Account add-on in the MultiClusterEngine CR.
	// oc patch multiclusterengine multiclusterengine --type='json' -p='[{"op": "add", "path": "/spec/overrides/components", "value": [{"name": "managedserviceaccount", "enabled": true}]}]'
	var multiclusterengineResourceGvr = schema.GroupVersionResource{
		Group:    "operator.open-cluster-management.io",
		Version:  "v1",
		Resource: "multiclusterengines",
	}

	mce, err := r.DynamicClient.Resource(multiclusterengineResourceGvr).Namespace("multicluster-engine").Get(ctx, "multicluster-engine", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "Failed to get MulticlusterEngine CR.")
	} else {
		log.Info("Got MCE cr.", "mce", mce)
	}

	// # Create configuration resources for each Managed Hub.
	// MANAGED_HUBS=($(oc get managedcluster -o json | jq -r '.items[] | select(.status.clusterClaims[] | .name == "hub.open-cluster-management.io" and .value != "NotInstalled") | .metadata.name'))
	var managedClusterResourceGvr = schema.GroupVersionResource{
		Group:    "cluster.open-cluster-management.io",
		Version:  "v1",
		Resource: "managedclusters",
	}
	clusterList, err := r.DynamicClient.Resource(managedClusterResourceGvr).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Error(err, "Failed to list ManagedClusters.")
	}

	for _, cluster := range clusterList.Items {
		isManagedHub := false
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

			// 1. Create a ManagedClusterAddon
			// Get the ManagedClusterAddon CRD
			managedClusterAddonResourceGvr := schema.GroupVersionResource{
				Group:    "addon.open-cluster-management.io",
				Version:  "v1alpha1",
				Resource: "managedclusteraddons",
			}
			_, err := r.DynamicClient.Resource(managedClusterAddonResourceGvr).Namespace(cluster.GetName()).Get(ctx, "managed-serviceaccount", metav1.GetOptions{})
			if err != nil {
				log.Info("ManagedClusterAddon managed-serviceaccount doesn't exist. Creating...")
			} else {
				log.Info("Found ManagedClusterAddon managed-serviceaccount.")
			}

			// 2. Create a ManagedServiceAccount
			managedServiceAccountGvr := schema.GroupVersionResource{
				Group:    "addon.open-cluster-management.io",
				Version:  "v1alpha1",
				Resource: "managedserviceaccounts",
			}
			_, err = r.DynamicClient.Resource(managedServiceAccountGvr).Namespace(cluster.GetName()).Get(ctx, "search-global", metav1.GetOptions{})
			if err != nil {
				log.Info("ManagedServiceAccount search-global doesn't exist. Creating...")
			} else {
				log.Info("Found ManagedServiceAccount search-global.")
			}

			// 3. Create a ManifestWork
			manifestWorkGvr := schema.GroupVersionResource{
				Group:    "work.open-cluster-management.io",
				Version:  "v1",
				Resource: "manifestworks",
			}
			_, err = r.DynamicClient.Resource(manifestWorkGvr).Namespace(cluster.GetName()).Get(ctx, "search-global-config", metav1.GetOptions{})
			if err != nil {
				log.Info("ManifestWork search-global-config doesn't exist. Creating...")
			} else {
				log.Info("Found ManifestWork search-global-config.")
			}
		}
	}

	return nil
}
