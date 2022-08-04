// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"time"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

func (r *SearchReconciler) deleteClusterManagementAddon(instance *searchv1alpha1.Search) error {
	log.V(2).Info("Deleting ClusterManagementAddon search-collector")
	cma := &addonapiv1alpha1.ClusterManagementAddOn{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterManagementAddon",
			APIVersion: "addon.open-cluster-management.io",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-collector",
			Namespace: instance.GetNamespace(),
		},
	}
	err := r.Delete(context.TODO(), cma)
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Failed to delete ClusterManagementAddon", "name", cma)
		return err
	}
	time.Sleep(1 * time.Second)
	log.V(2).Info("ClusterManagementAddon search-collector deleted", "name", cma)
	return nil
}
