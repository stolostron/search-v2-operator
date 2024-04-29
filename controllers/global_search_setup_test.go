// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	fake "k8s.io/client-go/dynamic/fake"
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

func Test_checkPrerequisites(t *testing.T) {
	// Create a fake client to mock API calls.
	s := scheme.Scheme
	fakeDynClient := fake.NewSimpleDynamicClientWithCustomListKinds(s,
		map[schema.GroupVersionResource]string{
			{Group: "operator.open-cluster-management.io", Version: "v1alpha4", Resource: "multiclusterglobalhubs"}: "MulticlusterGlobalHubList",
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
	)
	r := &SearchReconciler{
		Scheme:        s,
		DynamicClient: fakeDynClient,
	}

	ctx := context.Background()
	err := r.verifyGlobalSearchPrerequisites(ctx)
	if err != nil {
		t.Fatalf("Failed to verify global search prerequisites: %v", err)
	}

}

func Test_enableConsole(t *testing.T) {
	// Create a fake client to mock API calls.
	s := scheme.Scheme
	fakeConfigMap := newUnstructured("v1", "ConfigMap", "test-ns", "test-name")
	fakeConfigMap.Object["data"] = map[string]interface{}{}
	fakeDynClient := fake.NewSimpleDynamicClient(s, fakeConfigMap)

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
	fakeDynClient := fake.NewSimpleDynamicClient(s, fakeConfigMap)

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
