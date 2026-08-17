// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createUpdateRoles(ctx context.Context,
	crole *rbacv1.ClusterRole,
) (*reconcile.Result, error) {

	existingClusterRole := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      crole.Name,
		Namespace: crole.Namespace,
	}, existingClusterRole)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, crole)
		if err != nil {
			log.Error(err, "Could not create clusterrole "+crole.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created clusterrole " + crole.Name)
		log.V(9).Info("Created clusterrole ", "clusterrole", crole)
	} else if !equality.Semantic.DeepEqual(existingClusterRole.Rules, crole.Rules) {
		err = r.Update(ctx, crole)
		if err != nil {
			log.Error(err, "Could not update clusterrole "+crole.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Updated clusterrole " + crole.Name)
		log.V(9).Info("Updated clusterrole ", "clusterrole", crole)
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
		log.Info("Created clusterrolebinding " + rolebinding.Name)
		log.V(2).Info("Created clusterrolebinding ", "clusterrolebinding", rolebinding)
	} else if err == nil && !equality.Semantic.DeepEqual(found.Subjects, rolebinding.Subjects) {
		found.Subjects = rolebinding.Subjects
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "Could not update clusterrolebinding "+rolebinding.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Updated clusterrolebinding " + rolebinding.Name)
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

// APIClusterRole holds the impersonation rights that only search-api needs.
// It is bound exclusively to the search-api ServiceAccount so that the
// postgres, indexer and collector pods (which share search-serviceaccount)
// cannot escalate to system:masters via their mounted token.
func (r *SearchReconciler) APIClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
	cr := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: getRoleName() + "-api",
		},
		Rules: getAPIRules(),
	}
	if err := controllerutil.SetControllerReference(instance, cr, r.Scheme); err != nil {
		log.Info("Could not set control for ClusterRole " + cr.Name)
	}
	return cr
}

func (r *SearchReconciler) APIClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	crb := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: getRoleBindingName() + "-api",
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     getRoleName() + "-api",
			APIGroup: rbacv1.GroupName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      getAPIServiceAccountName(),
			Namespace: instance.GetNamespace(),
		}},
	}
	if err := controllerutil.SetControllerReference(instance, crb, r.Scheme); err != nil {
		log.Info("Could not set control for ClusterRoleBinding " + crb.Name)
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

func (r *SearchReconciler) GlobalSearchUserClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getSearchUserRoleName(),
			Namespace: instance.GetNamespace(),
		},
		Rules: getGlobalSearchUserRules(),
	}
}

func getSubjects(namespace string) []rbacv1.Subject {
	return []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      getServiceAccountName(),
			Namespace: namespace,
		},
		{
			Kind:      "ServiceAccount",
			Name:      getAPIServiceAccountName(),
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
			Verbs:     []string{"create", "get", "list", "watch", "patch", "update"},
		},
		{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"create", "get", "list", "watch", "patch", "update"},
		},
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     []string{"create", "get", "list", "watch", "patch", "update", "delete"},
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
			APIGroups: []string{"search.open-cluster-management.io"},
			Resources: []string{"collectorconfigs/status"},
			Verbs:     []string{"patch", "update"},
		},
	}
}

// getAPIRules returns the rules bound only to the search-api ServiceAccount.
// These are layered on top of getRules() (search-api is also bound to the
// shared "search" ClusterRole) and isolate the impersonation rights that
// search-api uses for user-scoped query authorization.
func getAPIRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"authentication.k8s.io", "authorization.k8s.io"},
			Resources: []string{"uids",
				"userextras/authentication.kubernetes.io/credential-id",
				"userextras/authentication.kubernetes.io/node-name",
				"userextras/authentication.kubernetes.io/node-uid",
				"userextras/authentication.kubernetes.io/pod-name",
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

func getMetricsRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods", "services", "endpoints"},
			Verbs:     []string{"get", "list", "watch"},
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

func getGlobalSearchUserRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{"search.open-cluster-management.io"},
			Resources: []string{"searches", "searches/allManagedData"},
			Verbs:     []string{"get"},
		},
	}
}
