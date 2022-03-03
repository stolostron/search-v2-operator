// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"

	cachev1 "github.com/stolostron/search-v2-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *OCMSearchReconciler) createSearchServiceAccount(request reconcile.Request,
	sa *corev1.ServiceAccount,
	instance *cachev1.OCMSearch,
) (*reconcile.Result, error) {

	found := &corev1.ServiceAccount{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      sa.Name,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), sa)
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

func (r *OCMSearchReconciler) SearchServiceAccount(instance *cachev1.OCMSearch) *corev1.ServiceAccount {

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
			Name: getImagePullSecret(),
		}},
	}

	err := controllerutil.SetControllerReference(instance, sa, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for %s serviceaccount", getServiceAccountName())
	}
	return sa
}
