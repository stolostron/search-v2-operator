// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *OCMSearchReconciler) createRoles(request reconcile.Request,
	role *rbacv1.ClusterRole,
	instance *cachev1.OCMSearch,
) (*reconcile.Result, error) {

	found := &rbacv1.ClusterRole{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleName(),
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), role)
		if err != nil {
			return &reconcile.Result{}, err
		} else {
			return nil, nil
		}
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	return nil, nil
}

func (r *OCMSearchReconciler) createRoleBinding(request reconcile.Request,
	rolebinding *rbacv1.ClusterRoleBinding,
	instance *cachev1.OCMSearch,
) (*reconcile.Result, error) {

	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      getRoleBindingName(),
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), rolebinding)
		if err != nil {
			return &reconcile.Result{}, err
		} else {
			return nil, nil
		}
	} else if err != nil {
		return &reconcile.Result{}, err
	}

	return nil, nil
}

func (r *OCMSearchReconciler) ClusterRole(instance *cachev1.OCMSearch) *rbacv1.ClusterRole {
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

func (r *OCMSearchReconciler) ClusterRoleBinding(instance *cachev1.OCMSearch) *rbacv1.ClusterRoleBinding {
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
