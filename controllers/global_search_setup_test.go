// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakeDyn "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newUnstructured(apiVersion, kind, namespace, name string, props map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetName(name)
	if namespace != "" {
		obj.SetNamespace(namespace)
	}
	if props != nil {
		for k, v := range props {
			obj.Object[k] = v
		}
	}
	return obj
}

func fakeDynClient() *fakeDyn.FakeDynamicClient {
	gvrToListKind, objects := defaultMockState()
	fakeDynClient := fakeDyn.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, gvrToListKind, objects...)
	return fakeDynClient
}

func defaultMockState() (map[schema.GroupVersionResource]string, []runtime.Object) {
	return map[schema.GroupVersionResource]string{
			{Group: "operator.open-cluster-management.io", Version: "v1alpha4", Resource: "multiclusterglobalhubs"}: "MulticlusterGlobalHubList",
			{Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters"}:               "ManagedClusterList",
			{Group: "multicluster.openshift.io", Version: "v1", Resource: "multiclusterengines"}:                    "MultiClusterEngineList",
			{Group: "work.open-cluster-management.io", Version: "v1", Resource: "manifestworks"}:                    "ManifestworkList",
			{Group: "authentication.open-cluster-management.io", Version: "v1", Resource: "managedserviceacounts"}:  "ManagedServiceAccountList",
		},
		[]runtime.Object{
			&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "operator.open-cluster-management.io",
					"kind":       "v1alpha4",
				},
				Items: []unstructured.Unstructured{
					*newUnstructured("operator.open-cluster-management.io/v1alpha4", "MulticlusterGlobalHub", "ns-foo", "name-foo", nil),
				},
			},
			&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "cluster.open-cluster-management.io/v1",
					"kind":       "ManagedCluster",
				},
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
					*newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-2", "cluster-2", nil),
				},
			},
			&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "multicluster.openshift.io/v1",
					"kind":       "MultiClusterEngine",
				},
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
							},
						},
					},
				},
			},
			&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "work.open-cluster-management.io/v1",
					"kind":       "Manifestwork",
				},
				Items: []unstructured.Unstructured{},
			},
			&unstructured.UnstructuredList{
				Object: map[string]interface{}{
					"apiVersion": "authentication.open-cluster-management.io/v1",
					"kind":       "ManagedServiceAccount",
				},
				Items: []unstructured.Unstructured{},
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

func Test_checkPrerequisites(t *testing.T) {
	// Create a fake client to mock API calls.
	r := &SearchReconciler{
		Scheme:        scheme.Scheme,
		DynamicClient: fakeDynClient(),
	}

	err := r.validateGlobalSearchPrerequisites(context.Background())

	assert.Nil(t, err)
}

func Test_enableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	fakeConfigMap := newUnstructured("v1", "ConfigMap", "test-ns", "test-name",
		map[string]interface{}{"data": map[string]interface{}{}})

	r := &SearchReconciler{
		Scheme:        scheme.Scheme,
		DynamicClient: fakeDyn.NewSimpleDynamicClient(scheme.Scheme, fakeConfigMap),
	}

	err := r.updateConsoleConfig(context.Background(), true, "test-ns", "test-name")

	assert.Nil(t, err)
}

func Test_disableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	fakeConfigMap := newUnstructured("v1", "ConfigMap", "test-ns", "test-name",
		map[string]interface{}{"data": map[string]interface{}{}})

	r := &SearchReconciler{
		Scheme:        scheme.Scheme,
		DynamicClient: fakeDyn.NewSimpleDynamicClient(scheme.Scheme, fakeConfigMap),
	}

	err := r.updateConsoleConfig(context.Background(), false, "test-ns", "test-name")

	assert.Nil(t, err)
}

func Test_enableGlobalSearch(t *testing.T) {
	// Create a fake client to mock API calls.
	searchInst := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-operator",
			Namespace: "open-cluster-management",
			Annotations: map[string]string{
				"global-search-preview": "true",
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
		DynamicClient: fakeDynClient(),
		Scheme:        scheme.Scheme,
	}

	_, err = r.reconcileGlobalSearch(context.Background(), searchInst)
	if err != nil {
		t.Fatalf("Failed to enable global search: %v", err)
	}

	assert.Equal(t, searchInst.Status.Conditions[0].Type, "GlobalSearchReady")
	assert.Equal(t, searchInst.Status.Conditions[0].Status, metav1.ConditionTrue)
	assert.Equal(t, searchInst.Spec.Deployments.QueryAPI.Env[0].Name, "FEATURE_FEDERATED_SEARCH")
	assert.Equal(t, searchInst.Spec.Deployments.QueryAPI.Env[0].Value, "true")
}

func Test_disableGlobalSearch(t *testing.T) {
	// Create a fake client to mock API calls.
	searchInst := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-operator",
			Namespace: "test-namespace",
			Annotations: map[string]string{
				"global-search-preview": "false",
			},
		},
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				QueryAPI: searchv1alpha1.DeploymentConfig{
					Env: []corev1.EnvVar{
						{
							Name:  "FEATURE_FEDERATED_SEARCH",
							Value: "true",
						},
					},
				},
			},
		},
		Status: searchv1alpha1.SearchStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "GlobalSearchReady",
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
		DynamicClient: fakeDynClient(),
		Scheme:        scheme.Scheme,
	}

	_, err = r.reconcileGlobalSearch(context.Background(), searchInst)

	assert.Nil(t, err)
	assert.Empty(t, searchInst.Status.Conditions)
	// FIXME: This assert is failing, need to fix bug in the implementation.
	assert.Empty(t, searchInst.Spec.Deployments.QueryAPI.Env)
}
