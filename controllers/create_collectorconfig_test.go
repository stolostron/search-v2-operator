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

// newIntegrationTeamConfig creates a CollectorConfig with the integration team label.
func newIntegrationTeamConfig(name string, spec searchv1alpha1.CollectorConfigSpec) *searchv1alpha1.CollectorConfig {
	cc := newCollectorConfig(name, spec)
	cc.Labels = map[string]string{
		integrationTeamLabel: integrationTeamLabelValue,
	}
	return cc
}

func setupReconciler(objs ...runtime.Object) *SearchReconciler {
	s := scheme.Scheme
	_ = searchv1alpha1.SchemeBuilder.AddToScheme(s)
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	return &SearchReconciler{Client: cl, Scheme: s}
}

func getIntegrationConfig(r *SearchReconciler) (*searchv1alpha1.CollectorConfig, error) {
	cc := &searchv1alpha1.CollectorConfig{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      integrationCollectorConfigName,
		Namespace: testNamespace,
	}, cc)
	return cc, err
}

func getMergedConfig(r *SearchReconciler) (*searchv1alpha1.CollectorConfig, error) {
	merged := &searchv1alpha1.CollectorConfig{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      mergedCollectorConfigName,
		Namespace: testNamespace,
	}, merged)
	return merged, err
}

// --- Integration team config discovery tests ---

func TestIntegration_MultipleTeamConfigs(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
				Fields:           []searchv1alpha1.Field{{Name: "severity", JSONPath: "{.spec.severity}"}},
			},
		},
	})
	teamB := newIntegrationTeamConfig("team-b-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "BAR",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"kubevirt.io"}, Kinds: []string{"VirtualMachine"}},
				Fields:           []searchv1alpha1.Field{{Name: "running", JSONPath: "{.spec.running}"}},
			},
		},
	})
	r := setupReconciler(instance, teamA, teamB)

	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Len(t, integrationCC.Spec.CollectionRules, 2)
}

func TestIntegration_ZeroTeamConfigs(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Empty(t, integrationCC.Spec.CollectionRules)
}

func TestIntegration_TeamConfigDeleted(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	teamB := newIntegrationTeamConfig("team-b-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "BAR",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"kubevirt.io"}, Kinds: []string{"VirtualMachine"}},
			},
		},
	})
	r := setupReconciler(instance, teamA, teamB)

	// First merge with both team configs.
	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Len(t, integrationCC.Spec.CollectionRules, 2)

	// Delete team-b.
	err = r.Delete(context.TODO(), teamB)
	assert.Nil(t, err)

	// Re-merge should have only team-a's rules.
	result, err = r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err = getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Len(t, integrationCC.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", integrationCC.Spec.CollectionRules[0].FieldSuffix)
}

func TestIntegration_Idempotency(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	r := setupReconciler(instance, teamA)

	// First call creates integration-collector-config.
	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	cc1, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	rv1 := cc1.ResourceVersion

	// Second call with no changes should not update.
	result, err = r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	cc2, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Equal(t, rv1, cc2.ResourceVersion)
}

func TestIntegration_DeterministicOrder(t *testing.T) {
	instance := newSearchInstance()
	// Create in reverse alphabetical order to verify sorting.
	teamZ := newIntegrationTeamConfig("z-team-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{Action: searchv1alpha1.ActionInclude, FieldSuffix: "Z", ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Pod"}}},
		},
	})
	teamA := newIntegrationTeamConfig("a-team-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{Action: searchv1alpha1.ActionInclude, FieldSuffix: "A", ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Service"}}},
		},
	})
	teamM := newIntegrationTeamConfig("m-team-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{Action: searchv1alpha1.ActionInclude, FieldSuffix: "M", ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"ConfigMap"}}},
		},
	})
	r := setupReconciler(instance, teamZ, teamA, teamM)

	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Len(t, integrationCC.Spec.CollectionRules, 3)
	assert.Equal(t, "A", integrationCC.Spec.CollectionRules[0].FieldSuffix)
	assert.Equal(t, "M", integrationCC.Spec.CollectionRules[1].FieldSuffix)
	assert.Equal(t, "Z", integrationCC.Spec.CollectionRules[2].FieldSuffix)
}

func TestIntegration_OnlyLabeledConfigsIncluded(t *testing.T) {
	instance := newSearchInstance()
	// Labeled integration team config — should be included.
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	// Unlabeled config — should NOT be included.
	unlabeled := newCollectorConfig("unlabeled-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Secret"}},
			},
		},
	})
	// Customer config — should NOT be included in integration merge.
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"ConfigMap"}},
			},
		},
	})
	r := setupReconciler(instance, teamA, unlabeled, customerCC)

	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Len(t, integrationCC.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", integrationCC.Spec.CollectionRules[0].FieldSuffix)
}

func TestIntegration_OwnerReference(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	integrationCC, err := getIntegrationConfig(r)
	assert.Nil(t, err)
	assert.Len(t, integrationCC.OwnerReferences, 1)
	assert.Equal(t, OperatorName, integrationCC.OwnerReferences[0].Name)
	assert.Equal(t, "Search", integrationCC.OwnerReferences[0].Kind)
}

// --- End-to-end merge tests (integration + customer → merged) ---

// runFullMerge calls both stages: integration team merge, then final merge.
func runFullMerge(r *SearchReconciler, instance *searchv1alpha1.Search) error {
	if result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance); err != nil || result != nil {
		return err
	}
	if result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance); err != nil || result != nil {
		return err
	}
	return nil
}

func TestMerge_IntegrationOnly(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
				Fields:           []searchv1alpha1.Field{{Name: "severity", JSONPath: "{.spec.severity}"}},
			},
		},
	})
	r := setupReconciler(instance, teamA)

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

func TestMerge_BothExist(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
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
	r := setupReconciler(instance, teamA, customerCC)

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	// Integration rules come first.
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Equal(t, searchv1alpha1.ActionExclude, merged.Spec.CollectionRules[1].Action)
}

func TestMerge_CustomerEmptyRules(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, teamA, customerCC)

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
}

func TestMerge_BothEmpty(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance) // No team configs, no customer config.

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Empty(t, merged.Spec.CollectionRules)
}

func TestMerge_CustomerCollectNamespaces(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
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
	r := setupReconciler(instance, teamA, customerCC)

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.NotNil(t, merged.Spec.CollectNamespaces)
	assert.Equal(t, []string{"production-*"}, merged.Spec.CollectNamespaces.NamespaceSelector.Include)
	assert.Equal(t, []string{"production-debug"}, merged.Spec.CollectNamespaces.NamespaceSelector.Exclude)
}

func TestMerge_NoCollectNamespaces(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	customerCC := newCollectorConfig(customerCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, teamA, customerCC)

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

func TestMerge_NoTeamConfigs(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	// With no team configs, integration-collector-config should still be created (empty).
	result, err := r.createOrUpdateIntegrationCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	// Merged should also work.
	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Empty(t, merged.Spec.CollectionRules)
}

func TestMerge_Idempotency(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"}},
			},
		},
	})
	r := setupReconciler(instance, teamA)

	// First full merge.
	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged1, err := getMergedConfig(r)
	assert.Nil(t, err)
	rv1 := merged1.ResourceVersion

	// Second full merge should not update (spec unchanged).
	err = runFullMerge(r, instance)
	assert.Nil(t, err)

	merged2, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Equal(t, rv1, merged2.ResourceVersion)
}

func TestMerge_CustomerDeleted(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				FieldSuffix:      "FOO",
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
	r := setupReconciler(instance, teamA, customerCC)

	// First merge with both.
	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	assert.NotNil(t, merged.Spec.CollectNamespaces)

	// Delete customer-collector-config.
	err = r.Delete(context.TODO(), customerCC)
	assert.Nil(t, err)

	// Re-merge, should revert to integration only.
	err = runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err = getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

func TestMerge_OwnerReference(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	err := runFullMerge(r, instance)
	assert.Nil(t, err)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.OwnerReferences, 1)
	assert.Equal(t, OperatorName, merged.OwnerReferences[0].Name)
	assert.Equal(t, "Search", merged.OwnerReferences[0].Kind)
}

// --- createCollectorConfig helper tests ---

func TestCreateCollectorConfig(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	cc := newCollectorConfig("test-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	result, err := r.createCollectorConfig(context.TODO(), cc)
	assert.Nil(t, err)
	assert.Nil(t, result)

	// Verify it was created.
	found := &searchv1alpha1.CollectorConfig{}
	err = r.Get(context.TODO(), types.NamespacedName{
		Name:      "test-config",
		Namespace: testNamespace,
	}, found)
	assert.Nil(t, err)
	assert.Equal(t, "test-config", found.Name)
}

func TestCreateCollectorConfig_AlreadyExists(t *testing.T) {
	instance := newSearchInstance()
	existingCC := newCollectorConfig("test-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, existingCC)

	cc := newCollectorConfig("test-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	result, err := r.createCollectorConfig(context.TODO(), cc)
	assert.Nil(t, err)
	assert.Nil(t, result)
}
