// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"errors"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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

// newCollectorConfig builds a bare CollectorConfig fixture with the given name and spec, with no
// labels or ownerReferences set.
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

// newIntegrationTeamConfig creates a CollectorConfig with the integration team label but no
// ownerReference — representing a team's differently-named test config (the manual-override
// escape hatch), which is user-authored and not reseeded by the operator.
func newIntegrationTeamConfig(name string, spec searchv1alpha1.CollectorConfigSpec) *searchv1alpha1.CollectorConfig {
	cc := newCollectorConfig(name, spec)
	cc.Labels = map[string]string{
		searchv1alpha1.IntegrationTeamLabel: searchv1alpha1.IntegrationTeamLabelValue,
	}
	return cc
}

// newOwnedIntegrationTeamConfig creates an integration team CollectorConfig with a controller
// ownerReference — representing a canonical config created by IntegrationCollectorConfigSeeder.
// These are deterministically reseeded on every operator restart and must never carry the
// backup label.
func newOwnedIntegrationTeamConfig(
	name string, spec searchv1alpha1.CollectorConfigSpec,
) *searchv1alpha1.CollectorConfig {
	cc := newIntegrationTeamConfig(name, spec)
	isController := true
	cc.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: searchv1alpha1.GroupVersion.String(),
		Kind:       "Search",
		Name:       OperatorName,
		UID:        "test-owner-uid",
		Controller: &isController,
	}}
	return cc
}

// newCollectorConfigTestScheme builds a scheme deliberately independent of the shared
// k8s.io/client-go/kubernetes/scheme.Scheme singleton that most of this package's other test
// files use (and mutate — e.g. global_search_setup_test.go's fakeDynClient registers
// ManagedClusterList against it as an *unstructured.UnstructuredList*). Reusing that singleton
// here would panic with a "double registration of different types" error the moment any test in
// this file runs after one of those, since triggerDistributionToAllClusters needs the *typed*
// clusterv1.ManagedClusterList registered instead. A dedicated scheme sidesteps that order
// dependency entirely, in both directions.
func newCollectorConfigTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = searchv1alpha1.SchemeBuilder.AddToScheme(s)
	// Registered so triggerDistributionToAllClusters's r.List(ctx, &clusterv1.ManagedClusterList{})
	// works against the fake client, even in tests that don't use any ManagedCluster fixtures.
	_ = clusterv1.AddToScheme(s)
	return s
}

func setupReconciler(objs ...runtime.Object) *SearchReconciler {
	s := newCollectorConfigTestScheme()
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&searchv1alpha1.CollectorConfig{}).
		Build()
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

// An operator-owned integration team CollectorConfig (the canonical, seeded kind — see
// IntegrationCollectorConfigSeeder) must NOT get the backup label: it is deterministically
// reseeded on every operator restart, and the label would break hub restore by conflicting
// with the CollectorConfig admission webhook's ownership check.
func TestBackupLabel_OwnedIntegrationTeamConfig_NotLabeled(t *testing.T) {
	instance := newSearchInstance()
	ownedCC := newOwnedIntegrationTeamConfig("cnv-integration", searchv1alpha1.CollectorConfigSpec{})
	r := setupReconciler(instance, ownedCC)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	updated := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: "cnv-integration", Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, updated))
	_, hasLabel := updated.Labels[backupLabel]
	assert.False(t, hasLabel, "operator-owned integration config must NOT have the backup label")
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

// When a user exclude is dropped due to integration team protection, user-collector-config
// gets Applied=False/RulesSkipped so the user can discover why their rule had no effect.
func TestMerge_UserExcludeDropped_StatusConditionSet(t *testing.T) {
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

	// Verify the status condition on user-collector-config reflects the dropped rule
	updated := &searchv1alpha1.CollectorConfig{}
	nn := types.NamespacedName{Name: userCollectorConfigName, Namespace: testNamespace}
	assert.Nil(t, r.Get(context.TODO(), nn, updated))
	require.Len(t, updated.Status.Conditions, 1)
	cond := updated.Status.Conditions[0]
	assert.Equal(t, searchv1alpha1.CollectorConfigConditionApplied, cond.Type)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, searchv1alpha1.CollectorConfigReasonRulesSkipped, cond.Reason)
	assert.Contains(t, cond.Message, "Deployment")
}

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
				Action: searchv1alpha1.ActionExclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{"coordination.k8s.io"},
					Kinds:     []string{"Lease"},
				},
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

// --- Prompt-reconcile trigger tests ---
//
// createOrUpdateMergedCollectorConfig calls triggerDistributionToAllClusters whenever
// merged-collector-config's spec actually changes, so addon-framework re-reconciles every
// cluster's search-collector addon promptly instead of waiting for its periodic resync. These
// tests substitute the package-level triggerCollectorConfigDistribution var (normally
// addon.TriggerSearchCollectorReconcile) with a spy, so they don't need a real addon manager.

// newManagedCluster builds a bare ManagedCluster fixture with the given name.
func newManagedCluster(name string) *clusterv1.ManagedCluster {
	return &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}

// stubTrigger substitutes the package-level triggerCollectorConfigDistribution var with a spy
// that records the cluster-name slice passed on every call, restoring the original value
// (addon.TriggerSearchCollectorReconcile) once the test completes so other tests are unaffected.
func stubTrigger(t *testing.T) *[][]string {
	t.Helper()
	original := triggerCollectorConfigDistribution
	calls := &[][]string{}
	triggerCollectorConfigDistribution = func(clusterNames []string) {
		*calls = append(*calls, append([]string{}, clusterNames...))
	}
	t.Cleanup(func() { triggerCollectorConfigDistribution = original })
	return calls
}

// On the very first call, merged-collector-config does not exist yet, so
// createOrUpdateMergedCollectorConfig takes the Create path. The trigger must fire exactly once,
// covering every existing ManagedCluster.
func TestTrigger_FiresOnCreate(t *testing.T) {
	instance := newSearchInstance()
	cluster1 := newManagedCluster("cluster1")
	cluster2 := newManagedCluster("cluster2")
	r := setupReconciler(instance, cluster1, cluster2)
	calls := stubTrigger(t)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	require.Len(t, *calls, 1, "expected exactly one trigger call on initial create")
	assert.ElementsMatch(t, []string{"cluster1", "cluster2"}, (*calls)[0])
}

// Once merged-collector-config exists, a real spec change (e.g. a new integration team config
// appearing) takes the Update path. The trigger must fire again, with the current cluster list.
func TestTrigger_FiresOnSpecChangeUpdate(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:      searchv1alpha1.ActionInclude,
				FieldSuffix: "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"},
				},
			},
		},
	})
	cluster1 := newManagedCluster("cluster1")
	r := setupReconciler(instance, teamA, cluster1)
	calls := stubTrigger(t)

	// First call creates merged-collector-config, which fires one trigger.
	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)
	require.Len(t, *calls, 1, "expected trigger on create")

	// Add a second team config so the next merge actually changes the spec.
	teamB := newIntegrationTeamConfig("team-b-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:      searchv1alpha1.ActionInclude,
				FieldSuffix: "BAR",
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{"kubevirt.io"}, Kinds: []string{"VirtualMachine"},
				},
			},
		},
	})
	require.Nil(t, r.Create(context.TODO(), teamB))

	result, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Nil(t, result)

	require.Len(t, *calls, 2, "expected a second trigger call after a real spec change")
	assert.Equal(t, []string{"cluster1"}, (*calls)[1])
}

// Mirrors TestMerge_Idempotency: a second call with no actual spec change must not fire a
// redundant trigger.
func TestTrigger_DoesNotFireOnNoOpUpdate(t *testing.T) {
	instance := newSearchInstance()
	teamA := newIntegrationTeamConfig("team-a-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:      searchv1alpha1.ActionInclude,
				FieldSuffix: "FOO",
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{"policy.open-cluster-management.io"}, Kinds: []string{"Policy"},
				},
			},
		},
	})
	r := setupReconciler(instance, teamA)
	calls := stubTrigger(t)

	// First call creates merged-collector-config, which fires one trigger.
	_, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	require.Len(t, *calls, 1)

	// Second call with no changes must not fire again.
	_, err = r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)
	assert.Len(t, *calls, 1, "no-op update must not trigger a redundant reconcile")
}

// When no ManagedClusters exist yet (e.g. a brand-new hub), the trigger is still called once —
// just with an empty slice — rather than being skipped entirely.
func TestTrigger_NoManagedClusters_CalledWithEmptySlice(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance)
	calls := stubTrigger(t)

	_, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)

	require.Len(t, *calls, 1)
	assert.Empty(t, (*calls)[0], "trigger should still be called, just with an empty cluster list")
}

// A failure listing ManagedClusters must not fail the reconcile, and must not call the trigger
// (there's nothing valid to pass it) — this is a best-effort optimization layered on top of the
// primary job of writing merged-collector-config, which must still succeed.
func TestTrigger_ListFailure_DoesNotFailReconcileOrTrigger(t *testing.T) {
	instance := newSearchInstance()
	s := newCollectorConfigTestScheme()
	base := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(instance).
		WithStatusSubresource(&searchv1alpha1.CollectorConfig{}).
		Build()
	listErr := errors.New("simulated API server error")
	failingClient := interceptor.NewClient(base, interceptor.Funcs{
		List: func(
			ctx context.Context, c client.WithWatch, obj client.ObjectList, opts ...client.ListOption,
		) error {
			if _, ok := obj.(*clusterv1.ManagedClusterList); ok {
				return listErr
			}
			return c.List(ctx, obj, opts...)
		},
	})
	r := &SearchReconciler{Client: failingClient, Scheme: s}
	calls := stubTrigger(t)

	result, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)

	assert.Nil(t, err, "a ManagedCluster list failure must not fail the overall reconcile")
	assert.Nil(t, result)
	assert.Empty(t, *calls, "trigger must not be called when listing ManagedClusters fails")

	merged, err := getMergedConfig(r)
	assert.Nil(t, err)
	assert.NotNil(t, merged, "merged-collector-config must still be written despite the trigger's list failure")
}

// Multiple existing ManagedClusters are all passed through to the trigger together, in a single
// call (not once per cluster) — asserting triggerDistributionToAllClusters's batching contract.
func TestTrigger_MultipleManagedClusters_AllPassedInOneCall(t *testing.T) {
	instance := newSearchInstance()
	r := setupReconciler(instance,
		newManagedCluster("cluster1"), newManagedCluster("cluster2"), newManagedCluster("cluster3"))
	calls := stubTrigger(t)

	_, err := r.createOrUpdateMergedCollectorConfig(context.TODO(), instance)
	assert.Nil(t, err)

	require.Len(t, *calls, 1, "all clusters should be delivered via a single trigger call")
	assert.ElementsMatch(t, []string{"cluster1", "cluster2", "cluster3"}, (*calls)[0])
}
