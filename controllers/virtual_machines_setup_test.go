// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakeDyn "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
			{Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters"}:              "ManagedClusterList",
			{Group: "multicluster.openshift.io", Version: "v1", Resource: "multiclusterengines"}:                   "MultiClusterEngineList",
			{Group: "rbac.open-cluster-management.io", Version: "v1alpha1", Resource: "clusterpermissions"}:        "ClusterPermissionList",
			{Group: "authentication.open-cluster-management.io", Version: "v1", Resource: "managedserviceacounts"}: "ManagedServiceAccountList",
		},
		[]runtime.Object{
			&unstructured.UnstructuredList{
				Object: buildObject("cluster.open-cluster-management.io/v1", "ManagedCluster"),
				Items: []unstructured.Unstructured{
					*newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-1", "cluster-1", nil),
					*newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-2", "cluster-2", nil),
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
				Object: buildObject("rbac.open-cluster-management.io/v1alpha1", "ClusterPermission"),
				Items: []unstructured.Unstructured{
					*newUnstructured("rbac.open-cluster-management.io/v1alpha1", "ClusterPermission", "cluster-1", "vm-actions", nil),
					*newUnstructured("rbac.open-cluster-management.io/v1alpha1", "ClusterPermission", "cluster-2", "vm-actions", nil),
				},
			},
			&unstructured.UnstructuredList{
				Object: buildObject("authentication.open-cluster-management.io/v1", "ManagedServiceAccount"),
				Items: []unstructured.Unstructured{
					*newUnstructured("authentication.open-cluster-management.io/v1", "ManagedServiceAccount", "cluster-1", "vm-actor", nil),
					*newUnstructured("authentication.open-cluster-management.io/v1", "ManagedServiceAccount", "cluster-2", "vm-actor", nil),
				},
			},
			newUnstructured("v1", "ConfigMap", "multicluster-engine", "console-mce-config",
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

func Test_VM_enableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	r := &SearchReconciler{
		Scheme:        scheme.Scheme,
		DynamicClient: fakeDynClientVM(),
	}

	err := r.updateConsoleConfigVM(context.Background(), true)
	assert.Nil(t, err)

	consoleMceConfig, _ := r.DynamicClient.Resource(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}).Namespace("multicluster-engine").Get(context.Background(), "console-mce-config", metav1.GetOptions{})
	assert.Equal(t, "enabled", consoleMceConfig.Object["data"].(map[string]interface{})["VIRTUAL_MACHINE_ACTIONS"])
}

func Test_VM_disableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	r := &SearchReconciler{
		Scheme:        scheme.Scheme,
		DynamicClient: fakeDynClientVM(),
	}

	err := r.updateConsoleConfigVM(context.Background(), false)
	assert.Nil(t, err)

	consoleMceConfig, _ := r.DynamicClient.Resource(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}).Namespace("multicluster-engine").Get(context.Background(), "console-mce-config", metav1.GetOptions{})
	assert.Nil(t, consoleMceConfig.Object["data"].(map[string]interface{})["VIRTUAL_MACHINE_ACTIONS"])
}

func Test_VM_enableActions(t *testing.T) {
	// Create a fake client to mock API calls.
	searchInst := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-operator",
			Namespace: "open-cluster-management",
			Annotations: map[string]string{
				"virtual-machine-preview": "true",
			},
		},
		Spec: searchv1alpha1.SearchSpec{},
	}

	err := searchv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	// Create a fake client to mock API calls.
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().WithStatusSubresource(searchInst).WithRuntimeObjects(searchInst).Build(),
		DynamicClient: fakeDynClientVM(),
		Scheme:        scheme.Scheme,
	}

	_, err = r.reconcileVirtualMachineConfiguration(context.Background(), searchInst)
	if err != nil {
		t.Fatalf("Failed to enable virtual machine actions: %v", err)
	}

	assert.NotEmpty(t, searchInst.Status.Conditions)
	assert.Equal(t, "VirtualMachineActionsReady", searchInst.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, searchInst.Status.Conditions[0].Status)
}

func Test_VM_disableActions(t *testing.T) {
	// Create a fake client to mock API calls.
	searchInst := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-operator",
			Namespace: "open-cluster-management",
			Annotations: map[string]string{
				"virtual-machine-preview": "false",
			},
		},
		Spec: searchv1alpha1.SearchSpec{},
		Status: searchv1alpha1.SearchStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "VirtualMachineActionsReady",
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
	err := searchv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	// Create a fake client to mock API calls.
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().WithStatusSubresource(searchInst).WithRuntimeObjects(searchInst).Build(),
		DynamicClient: fakeDynClientVM(),
		Scheme:        scheme.Scheme,
	}

	_, err = r.reconcileVirtualMachineConfiguration(context.Background(), searchInst)

	assert.Nil(t, err)
	assert.Empty(t, searchInst.Status.Conditions)
}
