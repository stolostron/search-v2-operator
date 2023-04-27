// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createAddOnDeploymentConfig(ctx context.Context,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	found := &addonv1alpha1.AddOnDeploymentConfig{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      getClusterManagementAddonName(),
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		adc := r.newAddOnDeploymentConfig(instance)
		err = r.Create(ctx, adc)
		if err != nil {
			log.Error(err, "Could not create AddOnDeploymentConfig")
			return &reconcile.Result{}, err
		}
		log.Info("Created AddOnDeploymentConfig " + adc.Name)
		return nil, nil
	}
	if err != nil {
		log.Error(err, "Error getting AddOnDeploymentConfig")
		return &reconcile.Result{}, err
	}
	return nil, nil
}

func (r *SearchReconciler) newAddOnDeploymentConfig(instance *searchv1alpha1.Search) *addonv1alpha1.AddOnDeploymentConfig {
	adc := &addonv1alpha1.AddOnDeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getClusterManagementAddonName(),
			Namespace: instance.GetNamespace(),
		},
		Spec: addonv1alpha1.AddOnDeploymentConfigSpec{
			NodePlacement: &addonv1alpha1.NodePlacement{},
		},
	}

	return adc
}
