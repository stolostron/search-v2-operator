// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	log.V(2).Info("ClusterManagementAddon search-collector deleted", "name", cma)
	return nil
}

func (r *SearchReconciler) deleteClusterRole(instance *searchv1alpha1.Search, resourceName string) error {
	log.V(2).Info("Deleting ClusterRole ", "resourceName", resourceName)
	cr := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: instance.GetNamespace(),
		},
	}
	err := r.Delete(context.TODO(), cr)
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Failed to delete ClusterRole", resourceName)
		return err
	}
	log.V(2).Info("Deleted ClusterRole", "ClusterRole", cr)
	return nil
}

func (r *SearchReconciler) deleteClusterRoleBinding(instance *searchv1alpha1.Search, resourceName string) error {
	log.V(2).Info("Deleting ClusterRoleBinding ", "resourceName", resourceName)
	crb := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      resourceName,
			Namespace: instance.GetNamespace(),
		},
	}
	err := r.Delete(context.TODO(), crb)
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Failed to delete ClusterRoleBiding", resourceName)
		return err
	}
	log.V(2).Info("Deleted ClusterRoleBinding", "ClusterRoleBinding", crb)
	return nil
}

func (r *SearchReconciler) deleteLegacyServiceMonitorSetup(instance *searchv1alpha1.Search) {
	var err error
	for _, sm := range []string{"search-api", "search-indexer"} {
		if err = r.Delete(r.context,
			r.ServiceMonitor(instance, sm, "openshift-monitoring")); err != nil && !errors.IsNotFound(err) {
			log.Error(err, "Failed to remove ServiceMonitor from openshift-monitoring namespace")
		}
	}
	if err := r.Delete(r.context, r.MetricsRole(instance)); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Failed to remove Role")
	}
	if err = r.Delete(r.context, r.MetricsRoleBinding(instance)); err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Failed to remove RoleBinding")
	}
	if err == nil {
		log.Info("Deleted legacy ServiceMonitor Setup from openshift-monitoring namespace")
	} else {
		log.Info("Failed to remove legacy ServiceMonitor setup from openshift-monitoring namespace")
	}
}
