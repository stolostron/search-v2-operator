// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	fakeDyn "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
	r := &SearchReconciler{
		Scheme:        s,
		DynamicClient: fakeDynClient(),
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
			Namespace: "test-namespace",
			Annotations: map[string]string{
				"search.open-cluster-management.io/global-search-preview": "true",
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
	err = r.enableGlobalSearch(ctx, searchInst)
	if err != nil {
		t.Fatalf("Failed to enable global search: %v", err)
	}

	// TODO: Check state.
}
