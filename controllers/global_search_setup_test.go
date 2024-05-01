// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	fakeDyn "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
)

func newUnstructured(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace": namespace,
				"name":      name,
			},
		},
	}
}

func fakeDynClient() *fakeDyn.FakeDynamicClient {
	s := scheme.Scheme
	fakeCM1 := newUnstructured("v1", "ConfigMap", "multicluster-engine", "console-mce-config")
	fakeCM1.Object["data"] = map[string]interface{}{"globalSearchFeatureFlag": "true"}
	fakeCM2 := newUnstructured("v1", "ConfigMap", "open-cluster-management", "console-config")
	fakeCM2.Object["data"] = map[string]interface{}{}

	fakeCluster1 := newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-1", "cluster-1")
	fakeCluster1.Object["status"] = map[string]interface{}{"clusterClaims": []interface{}{
		map[string]interface{}{"name": "hub.open-cluster-management.io", "value": "Installed"},
	}}
	fakeCluster2 := newUnstructured("cluster.open-cluster-management.io/v1", "ManagedCluster", "cluster-2", "cluster-2")

	fakeDynClient := fakeDyn.NewSimpleDynamicClientWithCustomListKinds(s,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "configmaps"}:                                                      "ConfigMapList",
			{Group: "operator.open-cluster-management.io", Version: "v1alpha4", Resource: "multiclusterglobalhubs"}: "MulticlusterGlobalHubList",
			{Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters"}:               "ManagedClusterList",
			{Group: "multicluster.openshift.io", Version: "v1", Resource: "multiclusterengines"}:                    "MultiClusterEngineList",
			{Group: "work.open-cluster-management.io", Version: "v1", Resource: "manifestworks"}:                    "ManifestworkList",
			{Group: "authentication.open-cluster-management.io", Version: "v1", Resource: "managedserviceacounts"}:  "ManagedServiceAccountList",
		},
		&unstructured.UnstructuredList{
			Object: map[string]interface{}{
				"apiVersion": "operator.open-cluster-management.io",
				"kind":       "v1alpha4",
			},
			Items: []unstructured.Unstructured{
				*newUnstructured("operator.open-cluster-management.io/v1alpha4", "MulticlusterGlobalHub", "ns-foo", "name-foo"),
			},
		},
		&unstructured.UnstructuredList{
			Object: map[string]interface{}{
				"apiVersion": "cluster.open-cluster-management.io/v1",
				"kind":       "ManagedCluster",
			},
			Items: []unstructured.Unstructured{
				*fakeCluster1, *fakeCluster2,
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
		fakeCM1, fakeCM2,
	)

	return fakeDynClient
}

func Test_checkPrerequisites(t *testing.T) {
	// Create a fake client to mock API calls.
	s := scheme.Scheme
	r := &SearchReconciler{
		Scheme:        s,
		DynamicClient: fakeDynClient(),
	}

	ctx := context.Background()
	err := r.validateGlobalSearchPrerequisites(ctx)
	if err != nil {
		t.Fatalf("Failed to verify global search prerequisites: %v", err)
	}

}

func Test_enableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	s := scheme.Scheme
	fakeConfigMap := newUnstructured("v1", "ConfigMap", "test-ns", "test-name")
	fakeConfigMap.Object["data"] = map[string]interface{}{}
	fakeDynClient := fakeDyn.NewSimpleDynamicClient(s, fakeConfigMap)

	r := &SearchReconciler{
		Scheme:        s,
		DynamicClient: fakeDynClient,
	}

	ctx := context.Background()
	err := r.updateConsoleConfig(ctx, true, "test-ns", "test-name")
	if err != nil {
		t.Fatalf("Failed to enable global search feature flag in console config: %v", err)
	}

}

func Test_disableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	s := scheme.Scheme
	fakeConfigMap := newUnstructured("v1", "ConfigMap", "test-ns", "test-name")
	fakeConfigMap.Object["data"] = map[string]interface{}{}
	fakeDynClient := fakeDyn.NewSimpleDynamicClient(s, fakeConfigMap)

	r := &SearchReconciler{
		Scheme:        s,
		DynamicClient: fakeDynClient,
	}

	ctx := context.Background()
	err := r.updateConsoleConfig(ctx, false, "test-ns", "test-name")
	if err != nil {
		t.Fatalf("Failed to disable global search feature flag in console config: %v", err)
	}
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
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	objs := []runtime.Object{searchInst}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(searchInst).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, DynamicClient: fakeDynClient(), Scheme: s}

	ctx := context.Background()
	_, err = r.reconcileGlobalSearch(ctx, searchInst)
	if err != nil {
		t.Fatalf("Failed to enable global search: %v", err)
	}

	//Check state here.
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
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	if err != nil {
		t.Errorf("error adding search scheme: (%v)", err)
	}

	objs := []runtime.Object{searchInst}
	// Create a fake client to mock API calls.
	cl := fake.NewClientBuilder().WithStatusSubresource(searchInst).WithRuntimeObjects(objs...).Build()

	r := &SearchReconciler{Client: cl, DynamicClient: fakeDynClient(), Scheme: s}

	ctx := context.Background()
	_, err = r.reconcileGlobalSearch(ctx, searchInst)
	if err != nil {
		t.Fatalf("Failed to disable global search: %v", err)
	}

	// Check state here.
	t.Log("search instance after: ", searchInst)
	// t.Fail()
}
