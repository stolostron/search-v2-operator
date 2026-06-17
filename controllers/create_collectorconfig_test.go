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
		searchv1alpha1.IntegrationTeamLabel: searchv1alpha1.IntegrationTeamLabelValue,
	}
	return cc
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

// --- Integration team discovery tests ---

func TestMerge_MultipleTeamConfigs(t *testing.T) {
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

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
}

func TestMerge_ZeroConfigs(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Empty(t, merged.Spec.CollectionRules)
}

func TestMerge_TeamConfigDeleted(t *testing.T) {
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
	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)

	// Delete team-b.
	err = r.Delete(context.TODO(), teamB)
	assert.Nil(t, err)

	// Re-merge should have only team-a's rules.
	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err = getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
}

func TestMerge_DeterministicOrder(t *testing.T) {
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

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 3)
	assert.Equal(t, "A", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Equal(t, "M", merged.Spec.CollectionRules[1].FieldSuffix)
	assert.Equal(t, "Z", merged.Spec.CollectionRules[2].FieldSuffix)
}

func TestMerge_OnlyLabeledConfigsIncluded(t *testing.T) {
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
	r := setupReconciler(instance, teamA, unlabeled)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
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

	// First call creates merged-collector-config.
	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged1, err := getMergedConfig(r)
	assert.Nil(t, err)
	rv1 := merged1.ResourceVersion

	// Second call with no changes should not update.
	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged2, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Equal(t, rv1, merged2.ResourceVersion)
}

// --- Integration + user merge tests ---

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

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

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
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Secret"}},
			},
		},
	})
	r := setupReconciler(instance, teamA, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	// Integration rules come first.
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Equal(t, searchv1alpha1.ActionExclude, merged.Spec.CollectionRules[1].Action)
}

func TestMerge_UserEmptyRules(t *testing.T) {
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
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, teamA, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
}

func TestMerge_UserCollectNamespaces(t *testing.T) {
	instance := newSearchInstance()
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
		CollectNamespaces: &searchv1alpha1.CollectNamespaces{
			NamespaceSelector: &searchv1alpha1.NamespaceSelector{
				Include: []string{"production-*"},
				Exclude: []string{"production-debug"},
			},
		},
	})
	r := setupReconciler(instance, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.NotNil(t, merged.Spec.CollectNamespaces)
	assert.Equal(t, []string{"production-*"}, merged.Spec.CollectNamespaces.NamespaceSelector.Include)
	assert.Equal(t, []string{"production-debug"}, merged.Spec.CollectNamespaces.NamespaceSelector.Exclude)
}

func TestMerge_NoCollectNamespaces(t *testing.T) {
	instance := newSearchInstance()
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{},
	})
	r := setupReconciler(instance, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

func TestMerge_UserDeleted(t *testing.T) {
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
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
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
	r := setupReconciler(instance, teamA, userCC)

	// First merge with both.
	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	assert.NotNil(t, merged.Spec.CollectNamespaces)

	// Delete user-collector-config.
	err = r.Delete(context.TODO(), userCC)
	assert.Nil(t, err)

	// Re-merge, should revert to integration only.
	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err = getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, "FOO", merged.Spec.CollectionRules[0].FieldSuffix)
	assert.Nil(t, merged.Spec.CollectNamespaces)
}

func TestMerge_CollectAdditionalPrinterColumnsPropagated(t *testing.T) {
	priority := 5
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:                                  searchv1alpha1.ActionInclude,
				ResourceSelector:                        searchv1alpha1.ResourceSelector{APIGroups: []string{"monitoring.coreos.com"}, Kinds: []string{"*"}},
				CollectAdditionalPrinterColumnsPriority: &priority,
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{""}, Kinds: []string{"Secret"}},
			},
		},
	})
	r := setupReconciler(instance, teamA, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2)
	// Integration rule with printer columns priority is propagated.
	assert.NotNil(t, merged.Spec.CollectionRules[0].CollectAdditionalPrinterColumnsPriority)
	assert.Equal(t, 5, *merged.Spec.CollectionRules[0].CollectAdditionalPrinterColumnsPriority)
	// User rule has no printer columns priority.
	assert.Nil(t, merged.Spec.CollectionRules[1].CollectAdditionalPrinterColumnsPriority)
}

func TestMerge_CollectAdditionalPrinterColumnsPriorityCollision(t *testing.T) {
	teamPriority := 10
	userPriority := 0
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:                                  searchv1alpha1.ActionInclude,
				ResourceSelector:                        searchv1alpha1.ResourceSelector{APIGroups: []string{"monitoring.coreos.com"}, Kinds: []string{"Alertmanager"}},
				CollectAdditionalPrinterColumnsPriority: &teamPriority,
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:                                  searchv1alpha1.ActionInclude,
				ResourceSelector:                        searchv1alpha1.ResourceSelector{APIGroups: []string{"monitoring.coreos.com"}, Kinds: []string{"Alertmanager"}},
				CollectAdditionalPrinterColumnsPriority: &userPriority,
			},
		},
	})
	r := setupReconciler(instance, teamA, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	// Both rules are preserved — integration first, user second.
	assert.Len(t, merged.Spec.CollectionRules, 2)
	assert.Equal(t, 10, *merged.Spec.CollectionRules[0].CollectAdditionalPrinterColumnsPriority,
		"Integration team rule should have priority 10")
	assert.Equal(t, 0, *merged.Spec.CollectionRules[1].CollectAdditionalPrinterColumnsPriority,
		"User rule should have priority 0")
}

func TestMerge_OwnerReference(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.OwnerReferences, 1)
	assert.Equal(t, OperatorName, merged.OwnerReferences[0].Name)
	assert.Equal(t, "Search", merged.OwnerReferences[0].Kind)
}

// Backup label tests — labeling now happens inside createOrUpdateMergedCollectorConfig
// to reuse the configs already fetched during the merge, avoiding a separate List call.

// User CollectorConfig without the backup label gets labeled during merge.
func TestBackupLabel_UserConfig_GetsLabeled(t *testing.T) {
	instance := newSearchInstance()
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{})
	r := setupReconciler(instance, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	updated := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: userCollectorConfigName, Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, updated))
	_, hasLabel := updated.Labels[backupLabel]
	assert.True(t, hasLabel, "user-collector-config should have the backup label")
}

// Integration team CollectorConfig without the backup label gets labeled during merge.
func TestBackupLabel_IntegrationTeamConfig_GetsLabeled(t *testing.T) {
	instance := newSearchInstance()
	teamCC := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{})
	r := setupReconciler(instance, teamCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	updated := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: "team-a-config", Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, updated))
	_, hasLabel := updated.Labels[backupLabel]
	assert.True(t, hasLabel, "integration team config should have the backup label")
}

// merged-collector-config is never passed to addBackupLabel — it is operator-managed and
// fully derived from source configs, so it should not carry the backup label.
func TestBackupLabel_MergedConfig_Skipped(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: mergedCollectorConfigName, Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, merged))
	_, hasLabel := merged.Labels[backupLabel]
	assert.False(t, hasLabel, "merged-collector-config should NOT have the backup label")
}

// A CollectorConfig that already has the backup label is not patched again.
func TestBackupLabel_AlreadyLabeled_NotPatched(t *testing.T) {
	instance := newSearchInstance()
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{})
	userCC.Labels = map[string]string{backupLabel: ""}
	userCC.ResourceVersion = "original"
	r := setupReconciler(instance, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	updated := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: userCollectorConfigName, Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, updated))
	assert.Equal(t, "original", updated.ResourceVersion, "ResourceVersion should be unchanged — no patch was applied")
}

// User and integration team configs both get labeled; merged-collector-config does not.
func TestBackupLabel_MultipleConfigs(t *testing.T) {
	instance := newSearchInstance()
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{})
	teamCC := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{})
	r := setupReconciler(instance, userCC, teamCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	for _, name := range []string{userCollectorConfigName, "team-a-config"} {
		cc := &searchv1alpha1.CollectorConfig{}
		nn := types.NamespacedName{Name: name, Namespace: testNamespace}
		assert.Nil(t, r.Get(context.TODO(), nn, cc))
		_, hasLabel := cc.Labels[backupLabel]
		assert.True(t, hasLabel, "%s should have the backup label", name)
	}

	merged := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: mergedCollectorConfigName, Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, merged))
	_, hasLabel := merged.Labels[backupLabel]
	assert.False(t, hasLabel, "merged-collector-config should NOT have the backup label")
}

// --- Exclude merge protection tests ---

// User exclude rule is dropped when integration team has an include for same kind.
func TestMerge_UserExcludeDroppedWhenIntegrationIncludes(t *testing.T) {
	instance := newSearchInstance()
	teamCC := newIntegrationTeamConfig("team-a", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
				Fields:           []searchv1alpha1.Field{{Name: "replicas", JSONPath: "{.spec.replicas}"}},
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
			},
		},
	})
	r := setupReconciler(instance, teamCC, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1)
	assert.Equal(t, searchv1alpha1.ActionInclude, merged.Spec.CollectionRules[0].Action)
}

// User exclude rule passes through when no integration team covers same kind.
func TestMerge_UserExcludeKeptWhenNoIntegrationOverlap(t *testing.T) {
	instance := newSearchInstance()
	teamCC := newIntegrationTeamConfig("team-a", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
				Fields:           []searchv1alpha1.Field{{Name: "replicas", JSONPath: "{.spec.replicas}"}},
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"coordination.k8s.io"}, Kinds: []string{"Lease"}},
			},
		},
	})
	r := setupReconciler(instance, teamCC, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2, "Both team include and user exclude should be in merged")
	assert.Equal(t, searchv1alpha1.ActionExclude, merged.Spec.CollectionRules[1].Action)
}

// User exclude with wildcard kind is dropped when integration team includes any kind in same group.
func TestMerge_UserWildcardExcludeDroppedWhenIntegrationIncludesInGroup(t *testing.T) {
	instance := newSearchInstance()
	teamCC := newIntegrationTeamConfig("team-a", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
				Fields:           []searchv1alpha1.Field{{Name: "replicas", JSONPath: "{.spec.replicas}"}},
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"*"}},
			},
		},
	})
	r := setupReconciler(instance, teamCC, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1, "User wildcard exclude overlapping team include should be dropped")
	assert.Equal(t, searchv1alpha1.ActionInclude, merged.Spec.CollectionRules[0].Action)
}

// Integration team wildcard include blocks specific user exclude.
func TestMerge_IntegrationWildcardIncludeBlocksSpecificUserExclude(t *testing.T) {
	instance := newSearchInstance()
	teamCC := newIntegrationTeamConfig("team-a", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"*"}},
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
			},
		},
	})
	r := setupReconciler(instance, teamCC, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 1,
		"User exclude of Deployment should be dropped — integration team includes all apps resources")
	assert.Equal(t, searchv1alpha1.ActionInclude, merged.Spec.CollectionRules[0].Action)
}

// User include rules are always passed through unchanged.
func TestMerge_UserIncludeNotAffectedByExcludeProtection(t *testing.T) {
	instance := newSearchInstance()
	teamCC := newIntegrationTeamConfig("team-a", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
				Fields:           []searchv1alpha1.Field{{Name: "replicas", JSONPath: "{.spec.replicas}"}},
			},
		},
	})
	userCC := newCollectorConfig(userCollectorConfigName, searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"apps"}, Kinds: []string{"Deployment"}},
				Fields:           []searchv1alpha1.Field{{Name: "strategy", JSONPath: "{.spec.strategy.type}"}},
			},
		},
	})
	r := setupReconciler(instance, teamCC, userCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.Len(t, merged.Spec.CollectionRules, 2, "Both include rules should be in merged — include is never filtered")
}
