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
	log.V(2).Info("Created %s serviceaccount", sa.Name)
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
		ImagePullSecrets: []corev1.LocalObjectReference{{
			Name: getImagePullSecretName(),
		}},
	}

	err := controllerutil.SetControllerReference(instance, sa, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for %s serviceaccount", getServiceAccountName())
	}
	return sa
}
