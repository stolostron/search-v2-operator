// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"io/fs"
	"sort"

	integrationconfigs "github.com/stolostron/search-v2-operator/config"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

// applyIntegrationCollectorConfigs creates or overwrites the initial integration team
// CollectorConfig CRs (CNV, OLM, GRC, Kyverno, Gatekeeper, Argo, ACM app lifecycle, etc.) from
// the manifests embedded in config/integration_collector_configs/. See ACM-37052.
//
// Integration teams contribute a plain CollectorConfig YAML file to that directory instead of
// writing Go code. This runs exactly once per operator process, at startup (see
// IntegrationCollectorConfigSeeder) — not on every reconcile. That is what lets a team edit their
// config directly and have it persist for the life of that pod: the canonical, fixed name always
// gets reset to whatever's currently embedded on the next pod start, but is left alone in
// between. A team that wants a change to survive across restarts before it ships officially
// creates a differently-named CollectorConfig (still carrying the integration label) instead of
// editing the canonical one — the merge step already discovers integration configs by label, not
// name, so it picks up any number of them automatically.
//
// Reads from the real embedded config/integration_collector_configs/ directory; see
// applyIntegrationCollectorConfigsFrom for the testable, FS-injectable version.
func applyIntegrationCollectorConfigs(
	ctx context.Context, c client.Client, scheme *runtime.Scheme, owner *searchv1alpha1.Search,
) error {
	return applyIntegrationCollectorConfigsFrom(ctx, c, scheme, owner, integrationconfigs.FS, integrationconfigs.Dir)
}

// applyIntegrationCollectorConfigsFrom is applyIntegrationCollectorConfigs with the filesystem and
// directory injected, so tests can exercise malformed-manifest and read-error paths with a
// fstest.MapFS instead of editing the real embedded YAMLs.
func applyIntegrationCollectorConfigsFrom(
	ctx context.Context, c client.Client, scheme *runtime.Scheme, owner *searchv1alpha1.Search,
	fsys fs.FS, dir string,
) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		log.Error(err, "Could not read embedded integration_collector_configs directory")
		return err
	}

	// Sort for deterministic, readable logs (fs.ReadDir is already sorted, but be explicit).
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	namespace := owner.Namespace
	var firstErr error
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := applyOneIntegrationCollectorConfig(ctx, c, scheme, owner, namespace, fsys, dir, entry.Name()); err != nil {
			// Log and continue — one bad or temporarily broken config must not prevent the
			// remaining integrations from being applied. The seeder retries the whole batch on
			// the next interval, so a transient failure here is self-healing.
			log.Error(err, "Failed to apply integration CollectorConfig, continuing with the rest", "file", entry.Name())
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// applyOneIntegrationCollectorConfig unconditionally creates or overwrites a single embedded
// integration CollectorConfig manifest — no diffing, no hash comparison. See
// applyIntegrationCollectorConfigsFrom for why an unconditional overwrite is safe here.
func applyOneIntegrationCollectorConfig(
	ctx context.Context, c client.Client, scheme *runtime.Scheme, owner *searchv1alpha1.Search,
	namespace string, fsys fs.FS, dir, filename string,
) error {
	raw, err := fs.ReadFile(fsys, dir+"/"+filename)
	if err != nil {
		log.Error(err, "Could not read embedded integration CollectorConfig", "file", filename)
		return err
	}

	desired := &searchv1alpha1.CollectorConfig{}
	if err := yaml.Unmarshal(raw, desired); err != nil {
		log.Error(err, "Could not parse embedded integration CollectorConfig", "file", filename)
		return err
	}
	if desired.Name == "" {
		log.Info("Skipping embedded integration CollectorConfig with no metadata.name", "file", filename)
		return nil
	}

	found := &searchv1alpha1.CollectorConfig{}
	err = c.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: namespace}, found)
	if errors.IsNotFound(err) {
		cc := &searchv1alpha1.CollectorConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CollectorConfig",
				APIVersion: searchv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      desired.Name,
				Namespace: namespace,
				Labels:    desired.Labels,
			},
			Spec: desired.Spec,
		}
		if cc.Labels == nil {
			cc.Labels = map[string]string{}
		}
		cc.Labels[searchv1alpha1.IntegrationTeamLabel] = searchv1alpha1.IntegrationTeamLabelValue
		// Set the Search CR as controller-owner so this config is garbage-collected when
		// Search is torn down, consistent with other operator-managed resources.
		if scheme != nil && owner != nil && owner.UID != "" {
			if err := controllerutil.SetControllerReference(owner, cc, scheme); err != nil {
				log.Error(err, "Could not set ownerReference on integration CollectorConfig", "name", cc.Name)
				return err
			}
		}
		if err := c.Create(ctx, cc); err != nil {
			log.Error(err, "Could not create integration CollectorConfig", "name", cc.Name)
			return err
		}
		log.Info("Created integration CollectorConfig", "name", cc.Name)
		return nil
	}
	if err != nil {
		return err
	}

	hasIntegrationLabel := found.Labels[searchv1alpha1.IntegrationTeamLabel] == searchv1alpha1.IntegrationTeamLabelValue
	hasOwnerRef := hasControllerOwnerRef(found, owner)
	if hasIntegrationLabel && hasOwnerRef && equality.Semantic.DeepEqual(found.Spec, desired.Spec) {
		// Already matches the currently shipped default, correctly labeled, and owned — skip.
		return nil
	}
	found.Spec = desired.Spec
	if found.Labels == nil {
		found.Labels = map[string]string{}
	}
	// Always (re-)set the label, even when only the spec differed — a pre-existing config found
	// without it would otherwise be invisible to the webhook's integration-overlap check and to
	// the merge step's label-based discovery, silently letting conflicting user excludes through.
	found.Labels[searchv1alpha1.IntegrationTeamLabel] = searchv1alpha1.IntegrationTeamLabelValue
	// Ensure ownerReference is set for GC when Search is torn down.
	if scheme != nil && owner != nil && owner.UID != "" && !hasOwnerRef {
		if err := controllerutil.SetControllerReference(owner, found, scheme); err != nil {
			log.Error(err, "Could not set ownerReference on integration CollectorConfig", "name", desired.Name)
			return err
		}
	}
	if err := c.Update(ctx, found); err != nil {
		log.Error(err, "Could not overwrite integration CollectorConfig", "name", desired.Name)
		return err
	}
	log.Info("Overwrote integration CollectorConfig with the currently shipped default", "name", desired.Name)
	return nil
}

// hasControllerOwnerRef returns true if obj already has a controller ownerReference pointing to
// the given owner. Used to avoid redundant updates.
func hasControllerOwnerRef(obj metav1.Object, owner *searchv1alpha1.Search) bool {
	if owner == nil || owner.UID == "" {
		return true // no owner to check against — treat as "already set" to avoid forcing updates in tests.
	}
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.UID {
			return true
		}
	}
	return false
}
