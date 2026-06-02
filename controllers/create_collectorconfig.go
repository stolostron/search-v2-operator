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

const (
	customerCollectorConfigName    = "customer-collector-config"
	integrationCollectorConfigName = "integration-collector-config"
	mergedCollectorConfigName      = "merged-collector-config"

	// integrationTeamLabel is the label key that integration teams apply to their CollectorConfig CRs
	// so the operator discovers and merges them into integration-collector-config.
	integrationTeamLabel      = "search.open-cluster-management.io/config-type"
	integrationTeamLabelValue = "integration"
)

// createOrUpdateIntegrationCollectorConfig discovers all CollectorConfig CRs labeled as integration
// team configs, merges their CollectionRules, and writes the result to integration-collector-config.
func (r *SearchReconciler) createOrUpdateIntegrationCollectorConfig(
	ctx context.Context,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	namespace := instance.GetNamespace()

	// List all integration team CollectorConfigs by label.
	teamConfigs := &searchv1alpha1.CollectorConfigList{}
	err := r.List(ctx, teamConfigs,
		client.InNamespace(namespace),
		client.MatchingLabels{integrationTeamLabel: integrationTeamLabelValue},
	)
	if err != nil {
		log.Error(err, "Could not list integration team CollectorConfigs")
		return &reconcile.Result{}, err
	}

	// Sort by name for deterministic merge order.
	sort.Slice(teamConfigs.Items, func(i, j int) bool {
		return teamConfigs.Items[i].Name < teamConfigs.Items[j].Name
	})

	// Merge all CollectionRules from team configs.
	var mergedRules []searchv1alpha1.CollectionRule
	for _, tc := range teamConfigs.Items {
		mergedRules = append(mergedRules, tc.Spec.CollectionRules...)
	}

	mergedSpec := searchv1alpha1.CollectorConfigSpec{
		CollectionRules: mergedRules,
	}
	// Ensure non-nil slice so DeepEqual works consistently.
	if mergedSpec.CollectionRules == nil {
		mergedSpec.CollectionRules = []searchv1alpha1.CollectionRule{}
	}

	// Get or create integration-collector-config.
	found := &searchv1alpha1.CollectorConfig{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      integrationCollectorConfigName,
		Namespace: namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		cc := &searchv1alpha1.CollectorConfig{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CollectorConfig",
				APIVersion: searchv1alpha1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      integrationCollectorConfigName,
				Namespace: namespace,
			},
			Spec: mergedSpec,
		}
		if errRef := controllerutil.SetControllerReference(instance, cc, r.Scheme); errRef != nil {
			log.V(2).Info("Could not set controller reference for integration-collector-config")
		}
		err = r.Create(ctx, cc)
		if err != nil {
			// FUTURE: detect and handle rule collisions between integration team configs. Leverage status conditions of erroneous config
			log.Error(err, "Could not create integration-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Created integration-collector-config", "ruleCount", len(mergedRules))
		return nil, nil
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	// Update only if the spec has changed.
	if !equality.Semantic.DeepEqual(found.Spec, mergedSpec) {
		found.Spec = mergedSpec
		if err := r.Update(ctx, found); err != nil {
			log.Error(err, "Could not update integration-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Updated integration-collector-config", "ruleCount", len(mergedRules))
	}

	return nil, nil
}

func (r *SearchReconciler) createCollectorConfig(
	ctx context.Context,
	cc *searchv1alpha1.CollectorConfig,
) (*reconcile.Result, error) {
	found := &searchv1alpha1.CollectorConfig{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      cc.Name,
		Namespace: cc.Namespace,
	}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			err = r.Create(ctx, cc)
			if err != nil {
				log.Error(err, "Could not create CollectorConfig", "name", cc.Name)
				return &reconcile.Result{}, err
			}
			log.V(2).Info("Created CollectorConfig", "name", cc.Name)
			return nil, nil
		}
		log.Error(err, "Could not get CollectorConfig", "name", cc.Name)
		return &reconcile.Result{}, err
	}
	return nil, nil
}

// createOrUpdateMergedCollectorConfig computes the union of integration-collector-config and
// customer-collector-config, then creates or updates merged-collector-config.
func (r *SearchReconciler) createOrUpdateMergedCollectorConfig(
	ctx context.Context,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	namespace := instance.GetNamespace()

	// Get integration-collector-config. If it doesn't exist, the operator hasn't created it yet.
	integrationCC := &searchv1alpha1.CollectorConfig{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      integrationCollectorConfigName,
		Namespace: namespace,
	}, integrationCC)
	if err != nil {
		if errors.IsNotFound(err) {
			log.V(2).Info("integration-collector-config not found, skipping merge")
			return nil, nil
		}
		return &reconcile.Result{}, err
	}

	// Get customer-collector-config. Not found is fine, customer may not have created one.
	customerCC := &searchv1alpha1.CollectorConfig{}
	customerExists := true
	err = r.Get(ctx, types.NamespacedName{
		Name:      customerCollectorConfigName,
		Namespace: namespace,
	}, customerCC)
	if err != nil {
		if errors.IsNotFound(err) {
			customerExists = false
		} else {
			return &reconcile.Result{}, err
		}
	}

	// Compute merged spec
	mergedSpec := searchv1alpha1.CollectorConfigSpec{}
	mergedSpec.CollectionRules = append(mergedSpec.CollectionRules, integrationCC.Spec.CollectionRules...)
	if customerExists {
		mergedSpec.CollectionRules = append(mergedSpec.CollectionRules, customerCC.Spec.CollectionRules...)
		if customerCC.Spec.CollectNamespaces != nil {
			mergedSpec.CollectNamespaces = customerCC.Spec.CollectNamespaces.DeepCopy()
		}
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
			log.V(2).Info("Could not set controller reference for merged-collector-config")
		}
		err = r.Create(ctx, merged)
		if err != nil {
			log.Error(err, "Could not create merged-collector-config")
			return &reconcile.Result{}, err
		}
		log.V(2).Info("Created merged-collector-config")
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
		log.V(2).Info("Updated merged-collector-config")
	}

	return nil, nil
}
