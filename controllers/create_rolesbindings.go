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
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Could not get clusterrole", "clusterrole", crole.Name)
		return &reconcile.Result{}, err
	}
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, crole)
		if err != nil {
			log.Error(err, "Could not create clusterrole "+crole.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created clusterrole " + crole.Name)
		log.V(9).Info("Created clusterrole ", "clusterrole", crole)
	} else if !equality.Semantic.DeepEqual(existingClusterRole.Rules, crole.Rules) {
		existingClusterRole.Rules = crole.Rules
		err = r.Update(ctx, existingClusterRole)
		if err != nil {
			log.Error(err, "Could not update clusterrole "+crole.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Updated clusterrole " + crole.Name)
		log.V(9).Info("Updated clusterrole ", "clusterrole", existingClusterRole)
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
	if err != nil && !errors.IsNotFound(err) {
		log.Error(err, "Could not get clusterrolebinding", "name", rolebinding.Name)
		return &reconcile.Result{}, err
	}
	if errors.IsNotFound(err) {
		err = r.Create(ctx, rolebinding)
		if err != nil {
			log.Error(err, "Could not create clusterrolebinding"+rolebinding.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Created clusterrolebinding " + rolebinding.Name)
		log.V(2).Info("Created clusterrolebinding ", "clusterrolebinding", rolebinding)
	} else {
		// ClusterRoleBinding.roleRef is immutable in Kubernetes. If the desired RoleRef
		// differs from the existing one (e.g. OPERATOR_ORG / OPERATOR_CHART changed between
		// releases), we must delete and recreate rather than update.
		if !equality.Semantic.DeepEqual(found.RoleRef, rolebinding.RoleRef) {
			log.Info("RoleRef changed — deleting and recreating clusterrolebinding", "name", rolebinding.Name,
				"old", found.RoleRef.Name, "new", rolebinding.RoleRef.Name)
			if err = r.Delete(ctx, found); err != nil {
				log.Error(err, "Could not delete stale clusterrolebinding "+rolebinding.Name)
				return &reconcile.Result{}, err
			}
			if err = r.Create(ctx, rolebinding); err != nil {
				log.Error(err, "Could not recreate clusterrolebinding "+rolebinding.Name)
				return &reconcile.Result{}, err
			}
			log.Info("Recreated clusterrolebinding " + rolebinding.Name)
		} else if !equality.Semantic.DeepEqual(found.Subjects, rolebinding.Subjects) {
			found.Subjects = rolebinding.Subjects
			if err = r.Update(ctx, found); err != nil {
				log.Error(err, "Could not update clusterrolebinding "+rolebinding.Name)
				return &reconcile.Result{}, err
			}
			log.Info("Updated clusterrolebinding " + rolebinding.Name)
		}
	}
	return nil, nil
}

// The "search-api" and "search-collector" ClusterRoles are pre-provisioned as static
// manifests. The operator creates only the ClusterRoleBindings that reference them,
// using the 'bind' verb so the operator SA never needs to hold impersonate or wildcard read.

func (r *SearchReconciler) APIClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	// Note: SetControllerReference is intentionally omitted for the same reason as
	// APIClusterRole. Cleanup is handled explicitly in finalizeSearch.
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: getRoleBindingName() + "-api",
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     getAPIClusterRoleName(),
			APIGroup: rbacv1.GroupName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      getAPIServiceAccountName(),
			Namespace: instance.GetNamespace(),
		}},
	}
}

// IndexerClusterRole holds the minimum permissions required by the search-indexer pod.
// Verified from source (pkg/server/authn.go, pkg/clustersync/leaderElection.go,
// pkg/clustersync/clusterSync.go):
//   - TokenReviews create — authn middleware validates incoming collector bearer tokens
//   - Leases get/create/update — leader election lock
//   - ManagedClusters, ManagedClusterInfos, ManagedClusterAddons get/list/watch — cluster-sync
//   - Discovery (ServerResourcesForGroupVersion) — CRD presence probes (covered by wildcard)
//
// The indexer does NOT use impersonate, secrets/configmap writes, or deployment verbs.
func (r *SearchReconciler) IndexerClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
	// Note: SetControllerReference is intentionally omitted. ClusterRole is cluster-scoped;
	// Search is namespaced. Kubernetes forbids a namespaced owner on a cluster-scoped
	// dependent, so the reference would always fail. Cleanup is handled explicitly in
	// finalizeSearch via deleteClusterRole.
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: getRoleName() + "-indexer",
		},
		Rules: getIndexerRules(),
	}
}

func (r *SearchReconciler) IndexerClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	// Note: SetControllerReference is intentionally omitted for the same reason as
	// IndexerClusterRole. Cleanup is handled explicitly in finalizeSearch.
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: getRoleBindingName() + "-indexer",
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     getRoleName() + "-indexer",
			APIGroup: rbacv1.GroupName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      getIndexerServiceAccountName(),
			Namespace: instance.GetNamespace(),
		}},
	}
}

// CollectorClusterRoleBinding binds the pre-provisioned search-collector ClusterRole
// (wildcard read) to the dedicated search-collector-sa ServiceAccount.
func (r *SearchReconciler) CollectorClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	// Note: SetControllerReference is intentionally omitted. ClusterRoleBinding is
	// cluster-scoped; Search is namespaced. Cleanup is handled explicitly in finalizeSearch.
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: getRoleBindingName() + "-collector",
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     getCollectorClusterRoleName(),
			APIGroup: rbacv1.GroupName,
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      getCollectorServiceAccountName(),
			Namespace: instance.GetNamespace(),
		}},
	}
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

// getIndexerRules returns the minimum permissions required by the search-indexer pod.
//
// Verified from source:
//   - pkg/server/authn.go:          TokenReviews().Create() — validates collector bearer tokens
//   - pkg/clustersync/leaderElection.go: LeaseLock — leader election
//   - pkg/clustersync/clusterSync.go:    dynamic informers + List for stale-cluster cleanup
//     watching ManagedClusters, ManagedClusterInfos, ManagedClusterAddons
//   - pkg/clustersync/clusterSync.go:    ServerResourcesForGroupVersion — CRD presence probe
//     (covered by the wildcard get/list/watch below)
//
// The indexer does NOT use impersonate, secret/configmap writes, or deployment verbs.
func getIndexerRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		// Authenticate incoming collector bearer tokens.
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"tokenreviews"},
			Verbs:     []string{"create"},
		},
		// Leader election lock.
		{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"get", "create", "update"},
		},
		// Cluster-sync: watch managed-cluster objects and check CRD presence via discovery.
		{
			APIGroups: []string{"cluster.open-cluster-management.io"},
			Resources: []string{"managedclusters"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"internal.open-cluster-management.io"},
			Resources: []string{"managedclusterinfos"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"addon.open-cluster-management.io"},
			Resources: []string{"managedclusteraddons"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}
}
