// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	customerCollectorConfigName    = "customer-collector-config"
	integrationCollectorConfigName = "integration-collector-config"
	mergedCollectorConfigName      = "merged-collector-config"
)

// IntegrationCollectorConfig returns the CollectorConfig managed by the operator for integration teams.
// This config is merged with the customer-collector-config to produce the merged-collector-config
// that the collector reads. Integration teams extend collection by adding rules to CollectionRules.
//
// Example: to collect a custom field from a Policy CRD, add a rule:
//
//	{
//		Action: searchv1alpha1.ActionInclude,
//		FieldSuffix: "GRC",
//		ResourceSelector: searchv1alpha1.ResourceSelector{
//			APIGroups: []string{"policy.open-cluster-management.io"},
//			Kinds:     []string{"Policy"},
//		},
//		Fields: []searchv1alpha1.Field{
//			{Name: "severity", JSONPath: "{.spec.severity}"},
//		},
//	}
//
// Note: Integration teams should always set a FieldSuffix to avoid collisions with customer-defined fields.
func (r *SearchReconciler) IntegrationCollectorConfig(
	instance *searchv1alpha1.Search,
) *searchv1alpha1.CollectorConfig {
	cc := &searchv1alpha1.CollectorConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CollectorConfig",
			APIVersion: searchv1alpha1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      integrationCollectorConfigName,
			Namespace: instance.GetNamespace(),
		},
		Spec: searchv1alpha1.CollectorConfigSpec{
			CollectionRules: []searchv1alpha1.CollectionRule{},
		},
	}

	err := controllerutil.SetControllerReference(instance, cc, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set controller reference for integration-collector-config")
	}
	return cc
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
