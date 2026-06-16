// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"sort"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ensureCollectorConfigsBackupLabel ensures that all user-managed CollectorConfig CRs in the
// operator namespace carry the ACM backup label so they survive a hub backup/restore cycle.
//
// The search.open-cluster-management.io API group is excluded from the automatic resources
// backup (backup.go excludedAPIGroups). Resources labeled with BackupLabel are picked up by
// the acm-resources-generic-schedule backup instead.
//
// merged-collector-config is intentionally skipped — it is operator-managed and rebuilt
// on every reconcile, so persisting it through backup would be redundant and confusing.
func (r *SearchReconciler) ensureCollectorConfigsBackupLabel(
	ctx context.Context,
	namespace string,
) (*reconcile.Result, error) {
	ccList := &searchv1alpha1.CollectorConfigList{}
	if err := r.List(ctx, ccList, client.InNamespace(namespace)); err != nil {
		log.Error(err, "Could not list CollectorConfigs for backup label check")
		return &reconcile.Result{}, err
	}

	for i := range ccList.Items {
		cc := &ccList.Items[i]
		if cc.Name == mergedCollectorConfigName {
			continue
		}
		if _, hasLabel := cc.Labels[backupLabel]; hasLabel {
			continue
		}
		patch := client.MergeFrom(cc.DeepCopy())
		if cc.Labels == nil {
			cc.Labels = map[string]string{}
		}
		cc.Labels[backupLabel] = ""
		if err := r.Patch(ctx, cc, patch); err != nil {
			log.Error(err, "Could not add backup label to CollectorConfig", "name", cc.Name)
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Added backup label to CollectorConfig", "name", cc.Name)
	}
	return nil, nil
}

const (
	userCollectorConfigName   = "user-collector-config"
	mergedCollectorConfigName = "merged-collector-config"

	// backupLabel is the ACM backup label that causes a resource to be included
	// in the acm-resources-generic-schedule backup. The search.open-cluster-management.io
	// API group is excluded from automatic backups, so CollectorConfig CRs need this
	// label to survive a hub backup/restore cycle.
	backupLabel = "cluster.open-cluster-management.io/backup"
)

// createOrUpdateMergedCollectorConfig discovers all integration team CollectorConfig CRs (by label)
// and the user-collector-config (by name), merges their CollectionRules, and writes the result
// to merged-collector-config.
func (r *SearchReconciler) createOrUpdateMergedCollectorConfig(
	ctx context.Context,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	namespace := instance.GetNamespace()

	// List all integration team CollectorConfigs by label.
	teamConfigs := &searchv1alpha1.CollectorConfigList{}
	err := r.List(ctx, teamConfigs,
		client.InNamespace(namespace),
		client.MatchingLabels{searchv1alpha1.IntegrationTeamLabel: searchv1alpha1.IntegrationTeamLabelValue},
	)
	if err != nil {
		log.Error(err, "Could not list integration team CollectorConfigs")
		return &reconcile.Result{}, err
	}

	// Sort by name for deterministic merge order.
	sort.Slice(teamConfigs.Items, func(i, j int) bool {
		return teamConfigs.Items[i].Name < teamConfigs.Items[j].Name
	})

	// FUTURE: detect and handle rule collisions between integration team configs.

	// Build merged spec: integration team rules first, then user rules.
	mergedSpec := searchv1alpha1.CollectorConfigSpec{}
	for _, tc := range teamConfigs.Items {
		mergedSpec.CollectionRules = append(mergedSpec.CollectionRules, tc.Spec.CollectionRules...)
	}

	// Get user-collector-config. Not found is fine, user may not have created one.
	userCC := &searchv1alpha1.CollectorConfig{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      userCollectorConfigName,
		Namespace: namespace,
	}, userCC)
	if err != nil && !errors.IsNotFound(err) {
		return &reconcile.Result{}, err
	}
	if err == nil {
		mergedSpec.CollectionRules = append(mergedSpec.CollectionRules, userCC.Spec.CollectionRules...)
		if userCC.Spec.CollectNamespaces != nil {
			mergedSpec.CollectNamespaces = userCC.Spec.CollectNamespaces.DeepCopy()
		}
	}

	// Ensure non-nil slice so DeepEqual works consistently.
	if mergedSpec.CollectionRules == nil {
		mergedSpec.CollectionRules = []searchv1alpha1.CollectionRule{}
	}

	// Get or create merged-collector-config.
	found := &searchv1alpha1.CollectorConfig{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      mergedCollectorConfigName,
		Namespace: namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		merged := &searchv1alpha1.CollectorConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CollectorConfig",
				APIVersion: searchv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      mergedCollectorConfigName,
				Namespace: namespace,
			},
			Spec: mergedSpec,
		}
		if errRef := controllerutil.SetControllerReference(instance, merged, r.Scheme); errRef != nil {
			log.Error(errRef, "Could not set controller reference for merged-collector-config")
			return &reconcile.Result{}, errRef
		}
		err = r.Create(ctx, merged)
		if err != nil {
			log.Error(err, "Could not create merged-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Created merged-collector-config", "ruleCount", len(mergedSpec.CollectionRules))
		return nil, nil
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	// Update only if the spec has changed.
	if !equality.Semantic.DeepEqual(found.Spec, mergedSpec) {
		found.Spec = mergedSpec
		if err := r.Update(ctx, found); err != nil {
			log.Error(err, "Could not update merged-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Updated merged-collector-config", "ruleCount", len(mergedSpec.CollectionRules))
	}

	return nil, nil
}
