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
	log.V(2).Info("Deleting ClusterRole ", resourceName)
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
	log.V(2).Info("Deleted ClusterRole", cr)
	return nil
}

func (r *SearchReconciler) deleteClusterRoleBinding(instance *searchv1alpha1.Search, resourceName string) error {
	log.V(2).Info("Deleting ClusterRoleBinding ", resourceName)
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
	log.V(2).Info("Deleted ClusterRoleBinding", crb)
	return nil
}
