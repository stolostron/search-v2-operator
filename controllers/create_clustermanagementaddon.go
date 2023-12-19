// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
		cma := r.newClusterManagementAddOn(instance)
		err = r.Create(ctx, cma)
		if err != nil {
			log.Error(err, "Could not create ClusterManagementAddOn")
			return &reconcile.Result{}, err
		}
		log.Info("Created ClusterManagementAddOn " + cma.Name)
		return nil, nil
	}
	if err != nil {
		log.Error(err, "Error getting ClusterManagementAddOn")
		return &reconcile.Result{}, err
	}
	return nil, nil
}

func (r *SearchReconciler) newClusterManagementAddOn(instance *searchv1alpha1.Search) *addonv1alpha1.ClusterManagementAddOn {
	cma := &addonv1alpha1.ClusterManagementAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getClusterManagementAddonName(),
			Namespace: instance.GetNamespace(),
		},
		Spec: addonv1alpha1.ClusterManagementAddOnSpec{
			AddOnMeta: addonv1alpha1.AddOnMeta{
				DisplayName: "Search Collector",
				Description: "Collects cluster data to be indexed by search components on the hub cluster.",
			},
			SupportedConfigs: []addonv1alpha1.ConfigMeta{
				{
					ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
						Group:    "addon.open-cluster-management.io",
						Resource: "addondeploymentconfigs",
					},
					DefaultConfig: &addonv1alpha1.ConfigReferent{
						Name:      getClusterManagementAddonName(),
						Namespace: instance.GetNamespace(),
					},
				},
			},
			InstallStrategy: addonv1alpha1.InstallStrategy{
				Type: addonv1alpha1.AddonInstallStrategyManual,
			},
		},
	}
	err := controllerutil.SetControllerReference(instance, cma, r.Scheme)
	if err != nil {
		log.Error(err, "Could not set control for search-collector ClusterManagementAddOn")
	}
	return cma
}
