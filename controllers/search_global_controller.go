// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	// oc patch configmap console-mce-config -n multicluster-engine -p '{"data": {"globalSearchFeatureFlag": "enabled"}}'
	consoleMceConfig := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: "console-mce-config", Namespace: "multicluster-engine"}, consoleMceConfig)
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

	// # Create configuration resources for each Managed Hub.
	// MANAGED_HUBS=($(oc get managedcluster -o json | jq -r '.items[] | select(.status.clusterClaims[] | .name == "hub.open-cluster-management.io" and .value != "NotInstalled") | .metadata.name'))
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
								// - apiVersion: route.openshift.io/v1
								//   kind: Route
								//   metadata:
								// 	labels:
								// 	  app: ocm-search
								// 	name: search-global-hub
								// 	namespace: open-cluster-management
								//   spec:
								// 	port:
								// 	  targetPort: search-api
								// 	tls:
								// 	  termination: passthrough
								// 	to:
								// 	  kind: Service
								// 	  name: search-search-api
								// 	  weight: 100
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
