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

func (r *SearchReconciler) createClusterManagementAddOn(ctx context.Context,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {
	found := &addonv1alpha1.ClusterManagementAddOn{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      getClusterManagementAddonName(),
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		cma := newClusterManagementAddOn(instance)
		err = r.Create(ctx, cma)
		if err != nil {
			log.Error(err, "Could not create ClusterManagementAddOn")
			return &reconcile.Result{}, err
		}
		log.Info("Created %s ClusterManagementAddOn", cma.Name)
		return nil, nil
	}
	if err != nil {
		log.Error(err, "Error getting ClusterManagementAddOn")
		return &reconcile.Result{}, err
	}
	return nil, nil
}

func newClusterManagementAddOn(instance *searchv1alpha1.Search) *addonv1alpha1.ClusterManagementAddOn {
	return &addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getClusterManagementAddonName(),
			Namespace: instance.GetNamespace(),
		},
		Spec: addonv1alpha1.ClusterManagementAddOnSpec{
			AddOnMeta: addonv1alpha1.AddOnMeta{
				DisplayName: "Search Collector",
				Description: "Collects cluster data to be indexed by search components on the hub cluster.",
			},
		},
	}
}
