// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "open-cluster-management"

func newSearchInstance() *searchv1alpha1.Search {
	return &searchv1alpha1.Search{
		TypeMeta: metav1.TypeMeta{Kind: "Search", APIVersion: searchv1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      OperatorName,
			Namespace: testNamespace,
		},
		Spec: searchv1alpha1.SearchSpec{},
	}
}

func newCollectorConfig(name string, spec searchv1alpha1.CollectorConfigSpec) *searchv1alpha1.CollectorConfig {
	return &searchv1alpha1.CollectorConfig{
		TypeMeta: metav1.TypeMeta{Kind: "CollectorConfig", APIVersion: searchv1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: spec,
	}
}

func setupReconciler(objs ...runtime.Object) *SearchReconciler {
	s := scheme.Scheme
	_ = searchv1alpha1.SchemeBuilder.AddToScheme(s)
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	return &SearchReconciler{Client: cl, Scheme: s}
}

func getMergedConfig(r *SearchReconciler) (*searchv1alpha1.CollectorConfig, error) {
	merged := &searchv1alpha1.CollectorConfig{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      mergedCollectorConfigName,
		Namespace: testNamespace,
	}, merged)
	return merged, err
}

// Test: only integration-collector-config exists -> merged = integration rules
func TestMerge_IntegrationOnly(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "GRC",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
				Fields:           []searchv1alpha1.Field{{Name: "severity", JSONPath: "{.spec.severity}"}},
			},
		},
	})
	r := setupReconciler(instance, integrationCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "GRC", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

// Test: both exist -> merged = union of both rule sets
func TestMerge_BothExist(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "GRC",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Secret"}},
			},
		},
	})
	r := setupReconciler(instance, integrationCC, customerCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	// Integration rules come first
	assert.Equal(t, "GRC", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Equal(t, searchv1alpha1.ActionExclude, merged.Spec.CollectionRules[1].Action)
}

// Test: customer-collector-config has empty rules -> merged = integration only
func TestMerge_CustomerEmptyRules(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "GRC",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, integrationCC, customerCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "GRC", merged.Spec.CollectionRules[0].FieldSuffix)
}

// Test: both empty -> merged has empty rules
func TestMerge_BothEmpty(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, integrationCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Empty(t, merged.Spec.CollectionRules)
}

// Test: customer-collector-config has collectNamespaces -> merged inherits it
func TestMerge_CustomerCollectNamespaces(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
		CollectNamespaces: &searchv1alpha1.CollectNamespaces{
			NamespaceSelector: &searchv1alpha1.NamespaceSelector{
				Include: []string{"production-*"},
				Exclude: []string{"production-debug"},
			},
		},
	})
	r := setupReconciler(instance, integrationCC, customerCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.NotNil(t, merged.Spec.CollectNamespaces)
	assert.Equal(t, []string{"production-*"}, merged.Spec.CollectNamespaces.NamespaceSelector.Include)
	assert.Equal(t, []string{"production-debug"}, merged.Spec.CollectNamespaces.NamespaceSelector.Exclude)
}

// Test: customer has no collectNamespaces -> merged has no namespace restriction
func TestMerge_NoCollectNamespaces(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, integrationCC, customerCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

// Test: integration-collector-config not found -> skip merge, no error
func TestMerge_IntegrationNotFound(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	// merged-collector-config should not exist
	_, err = getMergedConfig(r)
	assert.True(t, err != nil, "merged-collector-config should not exist")
}

// Test: idempotency, reconcile with no changes produces no update
func TestMerge_Idempotency(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "GRC",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	r := setupReconciler(instance, integrationCC)

	// First call creates merged-collector-config
	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged1, err := getMergedConfig(r)
	assert.Nil(t, err)
	rv1 := merged1.ResourceVersion

	// Second call should not update (spec unchanged)
	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged2, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Equal(t, rv1, merged2.ResourceVersion)
}

// Test: customer deleted after merge -> merged updates to integration only
func TestMerge_CustomerDeleted(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "GRC",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Secret"}},
			},
		},
		CollectNamespaces: &searchv1alpha1.CollectNamespaces{
			NamespaceSelector: &searchv1alpha1.NamespaceSelector{
				Include: []string{"prod-*"},
			},
		},
	})
	r := setupReconciler(instance, integrationCC, customerCC)

	// First merge with both configs
	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	assert.NotNil(t, merged.Spec.CollectNamespaces)

	// Delete customer-collector-config
	err = r.Delete(context.TODO(), customerCC)
	assert.Nil(t, err)

	// Re-merge, should revert to integration only
	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err = getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "GRC", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

// Test: merged-collector-config has owner reference to Search CR
func TestMerge_OwnerReference(t *testing.T) {
	instance := newSearchInstance()
	integrationCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, integrationCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.OwnerReferences, 1)
	assert.Equal(t, OperatorName, merged.OwnerReferences[0].Name)
	assert.Equal(t, "Search", merged.OwnerReferences[0].Kind)
}

// Test: createCollectorConfig creates integration-collector-config if not found
func TestCreateCollectorConfig(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	cc := r.IntegrationCollectorConfig(instance)
	result, err := r.createCollectorConfig(context.TODO(), cc)
	assert.Nil(t, err)
	assert.Nil(t, result)

	// Verify it was created
	found := &searchv1alpha1.CollectorConfig{}
	err = r.Get(context.TODO(), types.NamespacedName{
		Name:      integrationCollectorConfigName,
		Namespace: testNamespace,
	}, found)
	assert.Nil(t, err)
	assert.Equal(t, integrationCollectorConfigName, found.Name)
	assert.Empty(t, found.Spec.CollectionRules)
}

// Test: createCollectorConfig is idempotent, does not error if already exists
func TestCreateCollectorConfig_AlreadyExists(t *testing.T) {
	instance := newSearchInstance()
	existingCC := newCollectorConfig(integrationCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, existingCC)

	cc := r.IntegrationCollectorConfig(instance)
	result, err := r.createCollectorConfig(context.TODO(), cc)
	assert.Nil(t, err)
	assert.Nil(t, result)
}
