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
			Name: getRoleName() + "-api",
		},
		Rules: getAPIRules(),
	}
}

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
			Name:     getRoleName() + "-api",
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

// CollectorClusterRole holds the minimum permissions required by the search-collector
// deployment. The collector only needs to watch all resources (for inventory) and
// manage its own lease (for heartbeat). It does not need impersonate, write access
// to secrets/services/deployments, or any auth review verbs.
func (r *SearchReconciler) CollectorClusterRole(instance *searchv1alpha1.Search) *rbacv1.ClusterRole {
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
			Name: getRoleName() + "-collector",
		},
		Rules: getCollectorRules(),
	}
}

func (r *SearchReconciler) CollectorClusterRoleBinding(instance *searchv1alpha1.Search) *rbacv1.ClusterRoleBinding {
	// Note: SetControllerReference is intentionally omitted for the same reason as
	// CollectorClusterRole. Cleanup is handled explicitly in finalizeSearch.
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
			Name:     getRoleName() + "-collector",
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

// getCollectorRules returns the minimum permissions required by the search-collector pod.
//
// The collector's API surface (verified from source):
//   - Dynamic informers:  get/list/watch on all resources (*/*) for cluster inventory.
//   - Discovery client:   ServerPreferredResources — implicit; covered by the wildcard.
//   - ConfigMap read:     get on core/v1 ConfigMaps in its own namespace to read
//     search-collector-config (allow/deny resource filter).
//   - CollectorConfig:    get/list/watch on collectorconfigs.search.open-cluster-management.io
//     for configurable collection hot-reload.
//   - Lease management:   get/create/update on coordination.k8s.io/leases for the heartbeat
//     signal sent to the addon framework.
//
// The collector does NOT use: impersonate, secrets/services/deployments write,
// tokenreviews, selfsubjectaccessreviews, or collectorconfigs/status patch/update.
func getCollectorRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		// Watch every resource in the cluster for inventory collection.
		{
			APIGroups: []string{"*"},
			Resources: []string{"*"},
			Verbs:     []string{"get", "list", "watch"},
		},
		// Read the search-collector-config ConfigMap (allow/deny filter for resources).
		{
			APIGroups: []string{""},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get"},
		},
		// Watch CollectorConfig CRs for configurable-collection hot-reload.
		{
			APIGroups: []string{"search.open-cluster-management.io"},
			Resources: []string{"collectorconfigs"},
			Verbs:     []string{"get", "list", "watch"},
		},
		// Manage the addon heartbeat lease.
		{
			APIGroups: []string{"coordination.k8s.io"},
			Resources: []string{"leases"},
			Verbs:     []string{"get", "create", "update"},
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
