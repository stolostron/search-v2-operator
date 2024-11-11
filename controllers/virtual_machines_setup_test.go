// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakeDyn "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func fakeDynClientVM() *fakeDyn.FakeDynamicClient {
	gvrToListKind, objects := defaultMockStateVM()
	fakeDynClient := fakeDyn.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, gvrToListKind, objects...)
	return fakeDynClient
}

func defaultMockStateVM() (map[schema.GroupVersionResource]string, []runtime.Object) {
	buildObject := func(apiversion, kind string) map[string]interface{} {
		return map[string]interface{}{
			"apiVersion": apiversion,
			"kind":       kind,
		}
	}
	return map[schema.GroupVersionResource]string{
			{Group: "operator.open-cluster-management.io", Version: "v1alpha4", Resource: "multiclusterglobalhubs"}: "MulticlusterGlobalHubList",
			{Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters"}:               "ManagedClusterList",
			{Group: "multicluster.openshift.io", Version: "v1", Resource: "multiclusterengines"}:                    "MultiClusterEngineList",
			{Group: "work.open-cluster-management.io", Version: "v1", Resource: "manifestworks"}:                    "ManifestworkList",
			{Group: "authentication.open-cluster-management.io", Version: "v1", Resource: "managedserviceacounts"}:  "ManagedServiceAccountList",
		},
		[]runtime.Object{
			&unstructured.UnstructuredList{
				Object: buildObject("operator.open-cluster-management.io/v1alpha4", "MulticlusterGlobalHub"),
				Items: []unstructured.Unstructured{
					*newUnstructured("operator.open-cluster-management.io/v1alpha4", "MulticlusterGlobalHub", "ns-foo", "name-foo", nil),
				},
			},
			&unstructured.UnstructuredList{
				Object: buildObject("cluster.open-cluster-management.io/v1", "ManagedCluster"),
				Items: []unstructured.Unstructured{
					*newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-1", "cluster-1",
						map[string]interface{}{
							"status": map[string]interface{}{
								"clusterClaims": []interface{}{map[string]interface{}{
									"name":  "hub.open-cluster-management.io",
									"value": "Installed"},
								},
							},
						},
					),
					*newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-2", "cluster-2",
						map[string]interface{}{
							"status": map[string]interface{}{
								"clusterClaims": []interface{}{map[string]interface{}{
									"name":  "hub.open-cluster-management.io",
									"value": "NotInstalled"},
								},
							},
						},
					),
					*newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-3", "cluster-3", nil),
				},
			},
			&unstructured.UnstructuredList{
				Object: buildObject("multicluster.openshift.io/v1", "MultiClusterEngine"),
				Items: []unstructured.Unstructured{
					{
						Object: map[string]interface{}{
							"apiVersion": "multicluster.openshift.io/v1",
							"kind":       "MultiClusterEngine",
							"metadata": map[string]interface{}{
								"name": "multiclusterengine",
							},
							"spec": map[string]interface{}{
								"overrides": map[string]interface{}{
									"components": []interface{}{
										map[string]interface{}{
											"name":    "managedserviceaccount",
											"enabled": true,
										},
										map[string]interface{}{
											"name":    "cluster-proxy-addon",
											"enabled": true,
										},
									},
								},
								"targetNamespace": "multicluster-engine",
							},
						},
					},
				},
			},
			&unstructured.UnstructuredList{
				Object: buildObject("work.open-cluster-management.io/v1", "Manifestwork"),
				Items: []unstructured.Unstructured{
					*newUnstructured("work.open-cluster-management.io/v1", "Manifestwork", "cluster-1", "search-global-config", nil),
					*newUnstructured("work.open-cluster-management.io/v1", "Manifestwork", "cluster-2", "search-global-config", nil),
				},
			},
			&unstructured.UnstructuredList{
				Object: buildObject("authentication.open-cluster-management.io/v1", "ManagedServiceAccount"),
				Items: []unstructured.Unstructured{
					*newUnstructured("authentication.open-cluster-management.io/v1", "ManagedServiceAccount", "cluster-1", "search-global", nil),
					*newUnstructured("authentication.open-cluster-management.io/v1", "ManagedServiceAccount", "cluster-2", "search-global", nil),
				},
			},
			newUnstructured("v1", "ConfigMap", "multicluster-engine", "console-mce-config",
				map[string]interface{}{
					"data": map[string]interface{}{
						"globalSearchFeatureFlag": "true",
					},
				}),
			newUnstructured("v1", "ConfigMap", "open-cluster-management", "console-config",
				map[string]interface{}{
					"data": map[string]interface{}{},
				}),
		}
}

func Test_VM_checkPrerequisites(t *testing.T) {
	// Create a fake client to mock API calls.
	r := &SearchReconciler{
		Scheme:        scheme.Scheme,
		DynamicClient: fakeDynClientVM(),
	}

	err := r.validateVirtualMachineDependencies(context.Background())

	assert.Nil(t, err)
}

// func Test_VM_enableConsole(t *testing.T) {
// 	// Create a fake client to mock API calls.
// 	fakeConfigMap := newUnstructured("v1", "ConfigMap", "open-cluster-management", "console-config",
// 		map[string]interface{}{"data": map[string]interface{}{}})

// 	r := &SearchReconciler{
// 		Scheme:        scheme.Scheme,
// 		DynamicClient: fakeDyn.NewSimpleDynamicClient(scheme.Scheme, fakeConfigMap),
// 	}

// 	err := r.updateConsoleConfigVM(context.Background(), true, "open-cluster-management", "console-config")

// 	assert.Nil(t, err)
// }
