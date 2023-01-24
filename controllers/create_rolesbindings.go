// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createRoles(ctx context.Context,
	crole *rbacv1.ClusterRole,
) (*reconcile.Result, error) {

	found := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      crole.Name,
		Namespace: crole.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, crole)
		if err != nil {
			log.Error(err, "Could not create clusterrole "+crole.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created clusterrole" + crole.Name)
		log.V(9).Info("Created  clusterrole ", "clusterrole", crole)
	}
	return nil, nil
}

func (r *SearchReconciler) createRoleBinding(ctx context.Context,
	rolebinding *rbacv1.ClusterRoleBinding,
) (*reconcile.Result, error) {

	found := &rbacv1.ClusterRoleBinding{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      rolebinding.Name,
		Namespace: rolebinding.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, rolebinding)
		if err != nil {
			log.Error(err, "Could not create clusterrolebinding"+rolebinding.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created clusterrolebinding" + rolebinding.Name)
		log.V(2).Info("Created %s clusterrolebinding ", "clusterrolebinding", rolebinding)
	}
	return nil, nil
}

func (r *SearchReconciler) ClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
	cr := &rbacv1.ClusterRole{
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
	err := controllerutil.SetControllerReference(instance, cr, r.Scheme)
	if err != nil {
		log.Info("Could not set control for ClusterRole " + getRoleName())
	}
	return cr
}

func (r *SearchReconciler) ClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
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
	err := controllerutil.SetControllerReference(instance, crb, r.Scheme)
	if err != nil {
		log.Info("Could not set control for ClusterRoleBinding" + getRoleBindingName())
	}
	return crb
}

func (r *SearchReconciler) AddonClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getAddonRoleName(),
			Namespace: instance.GetNamespace(),
		},
		Rules: getAddonRules(),
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
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"create", "get", "list", "watch", "patch", "update"},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"*"},
		},
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"authorization.k8s.io"},
			Resources: []string{"selfsubjectaccessreviews", "selfsubjectrulesreviews"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"authentication.k8s.io", "authorization.k8s.io"},
			Resources: []string{"uids", "userextras/authentication.kubernetes.io/pod-name",
				"userextras/authentication.kubernetes.io/pod-uid"},
			Verbs: []string{"impersonate"},
		},
		{
			APIGroups: []string{""},
			Resources: []string{"users", "serviceaccounts", "groups"},
			Verbs:     []string{"impersonate"},
		},
	}
}

func getAddonRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"proxy.open-cluster-management.io"},
			Resources: []string{"clusterstatuses/aggregator"},
			Verbs:     []string{"create"},
		},
		{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"create", "get", "list", "watch", "patch", "update"},
		},
	}
}
