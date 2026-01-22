package controllers

import (
	"context"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func Test_enableFineGrainedRBAC(t *testing.T) {
	searchInst := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-operator",
			Namespace: "open-cluster-management",
			Annotations: map[string]string{
				"fine-grained-rbac": "true",
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

	_, err = r.reconcileFineGrainedRBACConfiguration(context.Background(), searchInst)

	assert.Nil(t, err)
	assert.NotEmpty(t, searchInst.Status.Conditions)
	assert.Equal(t, "FineGrainedRBACReady", searchInst.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, searchInst.Status.Conditions[0].Status)
}

func Test_disableFineGrainedRBAC(t *testing.T) {
	searchInst := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-operator",
			Namespace: "open-cluster-management",
			Annotations: map[string]string{
				"fine-grained-rbac-preview": "false",
			},
		},
		Spec: searchv1alpha1.SearchSpec{},
		Status: searchv1alpha1.SearchStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "FineGrainedRBACReady",
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

	_, err = r.reconcileFineGrainedRBACConfiguration(context.Background(), searchInst)

	assert.Nil(t, err)
	assert.Empty(t, searchInst.Status.Conditions)
}
