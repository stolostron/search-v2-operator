// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createRoles(request reconcile.Request,
	crole *rbacv1.ClusterRole,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {

	found := &rbacv1.ClusterRole{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleName(),
		Namespace: crole.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), crole)
		if err != nil {
			log.Error(err, "Could not create clusterrole")
			return &reconcile.Result{}, err
		}
	}
	log.V(2).Info("Created %s clusterrole", crole.Name)
	return nil, nil
}

func (r *SearchReconciler) createRoleBinding(request reconcile.Request,
	rolebinding *rbacv1.ClusterRoleBinding,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {

	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleBindingName(),
		Namespace: rolebinding.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(context.TODO(), rolebinding)
		if err != nil {
			log.Error(err, "Could not create clusterrolebinding")
			return &reconcile.Result{}, err
		}
	}
	log.V(2).Info("Created %s clusterrolebinding", rolebinding.Name)
	return nil, nil
}

func (r *SearchReconciler) ClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getRoleName(),
			Namespace: instance.GetNamespace(),
		},
		Rules: getRules(),
	}
}

func (r *SearchReconciler) ClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getRoleBindingName(),
			Namespace: instance.GetNamespace(),
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     getRoleName(),
			APIGroup: rbacv1.GroupName,
		},
		Subjects: getSubjects(instance.GetNamespace()),
	}
}

func getSubjects(namespace string) []rbacv1.Subject {
	return []rbacv1.Subject{{
		Kind:      "ServiceAccount",
		Name:      getServiceAccountName(),
		Namespace: namespace,
	},
	}
}

func getRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"secrets", "services"},
			Verbs:     []string{"*"},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"*"},
		},
	}
}
