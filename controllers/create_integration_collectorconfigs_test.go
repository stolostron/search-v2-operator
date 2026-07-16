// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// expectedIntegrationConfigNames must match metadata.name in each file under
// config/integration_collector_configs/. Kept explicit (rather than derived from the embedded
// FS) so a typo in a shipped YAML's name fails a test instead of silently creating nothing.
var expectedIntegrationConfigNames = []string{
	"cnv-integration-collector-config",
	"olm-integration-collector-config",
	"grc-integration-collector-config",
	"kyverno-integration-collector-config",
	"gatekeeper-integration-collector-config",
	"argo-integration-collector-config",
	"app-lifecycle-integration-collector-config",
}

func TestApplyIntegrationCollectorConfigs_CreatesAllEmbeddedConfigs(t *testing.T) {
	r := setupReconciler()

	err := applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace)
	require.NoError(t, err)

	for _, name := range expectedIntegrationConfigNames {
		cc := &searchv1alpha1.CollectorConfig{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: testNamespace}, cc)
		require.NoError(t, err, "expected %s to be created", name)
		assert.Equal(t, searchv1alpha1.IntegrationTeamLabelValue, cc.Labels[searchv1alpha1.IntegrationTeamLabel])
		assert.NotEmpty(t, cc.Spec.CollectionRules, "%s should have collection rules", name)
	}
}

func TestApplyIntegrationCollectorConfigs_NoOpWhenAlreadyUpToDate(t *testing.T) {
	r := setupReconciler()

	// First pass creates everything.
	require.NoError(t, applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace))

	before := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, before))

	// Second pass should be a no-op — same resourceVersion, same spec.
	require.NoError(t, applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace))

	after := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, after))
	assert.Equal(t, before.ResourceVersion, after.ResourceVersion, "unchanged config should not be updated")
}

// This is the deliberate tradeoff: applyIntegrationCollectorConfigs always resets the canonical
// name to the currently shipped default, even if it was customized. It's only ever called once
// per operator process at startup (see IntegrationCollectorConfigSeeder), not on every reconcile —
// so a customization survives for the life of the pod, but is reset on the next restart/upgrade.
// A team that wants a change to persist across restarts before it's officially shipped should use
// a different name for their CollectorConfig instead of editing the canonical one.
func TestApplyIntegrationCollectorConfigs_OverwritesCustomizedCanonicalConfig(t *testing.T) {
	customized := newIntegrationTeamConfig("cnv-integration-collector-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action: searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{"custom.example.io"}, Kinds: []string{"*"},
				},
			},
		},
	})
	r := setupReconciler(customized)

	require.NoError(t, applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace))

	after := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, after))
	assert.NotEqual(t, customized.Spec, after.Spec,
		"canonical config is reset to the shipped default on every seeder run, regardless of customization")
}

// If a canonical-named CollectorConfig exists without the integration label (e.g. pre-existing
// state from before this feature, or a bug elsewhere), the label must be added even when the spec
// already matches the shipped default — otherwise it stays invisible to the webhook's
// integration-overlap check and to the merge step's label-based discovery, silently allowing a
// conflicting user exclude through.
func TestApplyIntegrationCollectorConfigs_AddsMissingLabelWhenSpecAlreadyMatches(t *testing.T) {
	unlabeled := newCollectorConfig("cnv-integration-collector-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action: searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{
						"kubevirt.io", "cdi.kubevirt.io", "migrations.kubevirt.io", "clone.kubevirt.io",
						"instancetype.kubevirt.io", "snapshot.kubevirt.io",
						"networkaddonsoperator.network.kubevirt.io", "k8s.cni.cncf.io",
						"storage.k8s.io", "snapshot.storage.k8s.io", "snapshot.storage.kubevirt.io",
					},
					Kinds: []string{"*"},
				},
			},
		},
	})
	require.Empty(t, unlabeled.Labels, "test setup: must start with no labels at all")
	r := setupReconciler(unlabeled)

	require.NoError(t, applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace))

	after := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, after))
	assert.Equal(t, searchv1alpha1.IntegrationTeamLabelValue, after.Labels[searchv1alpha1.IntegrationTeamLabel],
		"label must be added even though the spec already matched the shipped default")
}

// Same as above, but the spec also differs — both the spec and the label must be fixed together.
func TestApplyIntegrationCollectorConfigs_AddsMissingLabelWhenSpecAlsoDiffers(t *testing.T) {
	unlabeled := newCollectorConfig("cnv-integration-collector-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"stale.example.io"}, Kinds: []string{"*"}},
			},
		},
	})
	require.Empty(t, unlabeled.Labels)
	r := setupReconciler(unlabeled)

	require.NoError(t, applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace))

	after := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, after))
	assert.Equal(t, searchv1alpha1.IntegrationTeamLabelValue, after.Labels[searchv1alpha1.IntegrationTeamLabel])
	assert.NotEqual(t, "stale.example.io", after.Spec.CollectionRules[0].ResourceSelector.APIGroups[0])
}

// A differently-named CollectorConfig (the escape hatch for mid-release testing/updates) is never
// touched by applyIntegrationCollectorConfigs — it only knows about the fixed set of names in
// config/integration_collector_configs/.
func TestApplyIntegrationCollectorConfigs_LeavesDifferentlyNamedConfigsAlone(t *testing.T) {
	testConfig := newIntegrationTeamConfig("cnv-integration-collector-config-2", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action: searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{
					APIGroups: []string{"mid-release-test.example.io"}, Kinds: []string{"*"},
				},
			},
		},
	})
	r := setupReconciler(testConfig)

	require.NoError(t, applyIntegrationCollectorConfigs(context.TODO(), r.Client, testNamespace))

	after := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config-2", Namespace: testNamespace,
	}, after))
	assert.Equal(t, testConfig.Spec, after.Spec, "differently-named configs are never touched")
}

// --- Edge cases exercised via an injected fstest.MapFS, since the real embedded YAMLs are
// always well-formed and can't exercise these defensive branches on their own. ---

func validCollectorConfigYAML(name string) []byte {
	return []byte(`
apiVersion: search.open-cluster-management.io/v1alpha1
kind: CollectorConfig
metadata:
  name: ` + name + `
spec:
  collectionRules:
    - action: include
      resourceSelector:
        apiGroups: ["example.io"]
        kinds: ["*"]
`)
}

func TestApplyIntegrationCollectorConfigsFrom_ReadDirError(t *testing.T) {
	r := setupReconciler()
	fsys := fstest.MapFS{} // "configs" directory does not exist.

	err := applyIntegrationCollectorConfigsFrom(context.TODO(), r.Client, testNamespace, fsys, "configs")
	assert.Error(t, err)
}

func TestApplyIntegrationCollectorConfigsFrom_SkipsSubdirectories(t *testing.T) {
	r := setupReconciler()
	// fstest.MapFS automatically synthesizes a directory entry for "configs/subdir" because
	// "configs/subdir/nested.yaml" exists — no need to set it explicitly. ReadDir("configs")
	// returns both "a-real-file.yaml" and the "subdir" directory entry, exercising the
	// entry.IsDir() skip in applyIntegrationCollectorConfigsFrom.
	fsys := fstest.MapFS{
		"configs/a-real-file.yaml":   &fstest.MapFile{Data: validCollectorConfigYAML("real-config")},
		"configs/subdir/nested.yaml": &fstest.MapFile{Data: validCollectorConfigYAML("nested-config")},
	}
	err := applyIntegrationCollectorConfigsFrom(context.TODO(), r.Client, testNamespace, fsys, "configs")
	require.NoError(t, err)

	cc := &searchv1alpha1.CollectorConfig{}
	realConfigKey := types.NamespacedName{Name: "real-config", Namespace: testNamespace}
	require.NoError(t, r.Get(context.TODO(), realConfigKey, cc))
	// The nested file's config should NOT have been applied, since ReadDir on "configs" only
	// lists immediate children ("a-real-file.yaml" and the "subdir" directory itself, not
	// "subdir/nested.yaml"), and directories are skipped.
	nestedConfigKey := types.NamespacedName{Name: "nested-config", Namespace: testNamespace}
	err = r.Get(context.TODO(), nestedConfigKey, &searchv1alpha1.CollectorConfig{})
	assert.True(t, apierrors.IsNotFound(err), "nested file must not be read directly out of a subdirectory")
}

func TestApplyIntegrationCollectorConfigsFrom_StopsOnFirstFileError(t *testing.T) {
	r := setupReconciler()
	fsys := fstest.MapFS{
		"configs/a-bad.yaml":  &fstest.MapFile{Data: []byte("not: valid: yaml: [")},
		"configs/z-good.yaml": &fstest.MapFile{Data: validCollectorConfigYAML("good-config")},
	}

	err := applyIntegrationCollectorConfigsFrom(context.TODO(), r.Client, testNamespace, fsys, "configs")
	assert.Error(t, err, "a malformed manifest must fail the whole pass, not be silently skipped")

	// Files are processed in sorted order, so "a-bad.yaml" (alphabetically first) fails before
	// "z-good.yaml" is ever reached.
	goodConfigKey := types.NamespacedName{Name: "good-config", Namespace: testNamespace}
	err = r.Get(context.TODO(), goodConfigKey, &searchv1alpha1.CollectorConfig{})
	assert.True(t, apierrors.IsNotFound(err), "processing stops at the first error, later files are never applied")
}

func TestApplyOneIntegrationCollectorConfig_SkipsManifestWithNoName(t *testing.T) {
	r := setupReconciler()
	fsys := fstest.MapFS{
		"configs/no-name.yaml": &fstest.MapFile{Data: []byte(`
apiVersion: search.open-cluster-management.io/v1alpha1
kind: CollectorConfig
spec:
  collectionRules: []
`)},
	}

	err := applyIntegrationCollectorConfigsFrom(context.TODO(), r.Client, testNamespace, fsys, "configs")
	assert.NoError(t, err, "a manifest with no metadata.name is skipped, not an error")
}

func TestApplyOneIntegrationCollectorConfig_MalformedYAMLReturnsError(t *testing.T) {
	r := setupReconciler()
	fsys := fstest.MapFS{
		"configs/broken.yaml": &fstest.MapFile{Data: []byte("not: valid: yaml: [")},
	}

	err := applyIntegrationCollectorConfigsFrom(context.TODO(), r.Client, testNamespace, fsys, "configs")
	assert.Error(t, err)
}

func TestApplyOneIntegrationCollectorConfig_GetErrorOtherThanNotFound(t *testing.T) {
	base := setupReconciler().Client.(client.WithWatch)
	failingClient := interceptor.NewClient(base, interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			return errors.New("simulated API server error")
		},
	})

	err := applyIntegrationCollectorConfigs(context.TODO(), failingClient, testNamespace)
	assert.Error(t, err)
}

func TestApplyOneIntegrationCollectorConfig_CreateError(t *testing.T) {
	base := setupReconciler().Client.(client.WithWatch)
	failingClient := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			return errors.New("simulated create failure")
		},
	})

	err := applyIntegrationCollectorConfigs(context.TODO(), failingClient, testNamespace)
	assert.Error(t, err)
}

func TestApplyOneIntegrationCollectorConfig_UpdateError(t *testing.T) {
	// Pre-create one config with a spec that differs from the shipped default, so the code
	// takes the Update path rather than Create.
	existing := newIntegrationTeamConfig("cnv-integration-collector-config", searchv1alpha1.CollectorConfigSpec{
		CollectionRules: []searchv1alpha1.CollectionRule{
			{
				Action:           searchv1alpha1.ActionInclude,
				ResourceSelector: searchv1alpha1.ResourceSelector{APIGroups: []string{"stale.example.io"}, Kinds: []string{"*"}},
			},
		},
	})
	base := setupReconciler(existing).Client.(client.WithWatch)
	failingClient := interceptor.NewClient(base, interceptor.Funcs{
		Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			return errors.New("simulated update failure")
		},
	})

	err := applyIntegrationCollectorConfigs(context.TODO(), failingClient, testNamespace)
	assert.Error(t, err)
}
