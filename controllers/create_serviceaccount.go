// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createSearchServiceAccount(ctx context.Context,
	sa *corev1.ServiceAccount,
) (*reconcile.Result, error) {

	found := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      sa.Name,
		Namespace: sa.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {
		err = r.Create(ctx, sa)
		if err != nil {
			log.Error(err, "Could not create serviceaccount")
			return &reconcile.Result{}, err
		}
	}
	if len(found.ImagePullSecrets) > 1 {
		if err = r.Update(ctx, sa); err != nil {
			log.Error(err, "Could not update serviceaccount "+sa.Name)
			return &reconcile.Result{}, err
		}
		log.Info("Updated serviceaccount " + sa.Name)
	}
	log.V(2).Info("Created serviceaccount", "ServiceAccount", sa.Name)
	return nil, nil
}

func (r *SearchReconciler) SearchServiceAccount(instance *searchv1alpha1.Search) *corev1.ServiceAccount {

	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getServiceAccountName(),
			Namespace: instance.GetNamespace(),
		},
	}

	err := controllerutil.SetControllerReference(instance, sa, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for ", "serviceaccount", getServiceAccountName())
	}
	return sa
}

// SearchPostgresServiceAccount builds the dedicated SA for the search-postgres
// deployment. Postgres makes no Kubernetes API calls, so no ClusterRole is bound
// to this SA — it exists solely to isolate the pod identity from the other
// search components.
// An error is returned when the owner reference cannot be set so that the
// reconcile loop can abort before creating an ownerless ServiceAccount.
func (r *SearchReconciler) SearchPostgresServiceAccount(instance *searchv1alpha1.Search) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getPostgresServiceAccountName(),
			Namespace: instance.GetNamespace(),
		},
	}
	if err := controllerutil.SetControllerReference(instance, sa, r.Scheme); err != nil {
		return nil, err
	}
	return sa, nil
}

// SearchIndexerServiceAccount builds the dedicated SA for the search-indexer
// deployment. It is bound only to the search-indexer ClusterRole, which grants
// the minimum permissions required: TokenReview create, lease management, and
// read access to the ACM cluster-sync resources.
// An error is returned when the owner reference cannot be set so that the
// reconcile loop can abort before creating an ownerless ServiceAccount.
func (r *SearchReconciler) SearchIndexerServiceAccount(instance *searchv1alpha1.Search) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getIndexerServiceAccountName(),
			Namespace: instance.GetNamespace(),
		},
	}
	if err := controllerutil.SetControllerReference(instance, sa, r.Scheme); err != nil {
		return nil, err
	}
	return sa, nil
}

// SearchAPIServiceAccount builds the dedicated SA for the search-api
// deployment. It is the only subject bound to the search-api ClusterRole
// carrying impersonate rights.
func (r *SearchReconciler) SearchAPIServiceAccount(instance *searchv1alpha1.Search) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      getAPIServiceAccountName(),
			Namespace: instance.GetNamespace(),
		},
	}
	if err := controllerutil.SetControllerReference(instance, sa, r.Scheme); err != nil {
		log.V(2).Info("Could not set control for ", "serviceaccount", getAPIServiceAccountName())
	}
	return sa
}
