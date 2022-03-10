// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"crypto/rand"
	"math/big"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (r *SearchReconciler) createPGSecret(request reconcile.Request,
	secret *corev1.Secret,
	instance *searchv1alpha1.Search,
) (*reconcile.Result, error) {

	found := &corev1.Secret{}
	err := r.Get(context.TODO(), types.NamespacedName{
		Name:      secret.Name,
		Namespace: instance.Namespace,
	}, found)
	if err != nil && errors.IsNotFound(err) {

		err = r.Create(context.TODO(), secret)
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

func (r *SearchReconciler) PGSecret(instance *searchv1alpha1.Search) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-postgres",
			Namespace: instance.GetNamespace(),
		},
		Type: corev1.SecretTypeOpaque,
	}
	secret.StringData = map[string]string{
		"database-user":     "searchuser",
		"database-password": generatePass(16),
		"database-name":     "search",
	}

	err := controllerutil.SetControllerReference(instance, secret, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres secret")
	}
	return secret
}
func generatePass(length int) string {
	chars := "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789"

	buf := make([]byte, length)
	for i := 0; i < length; i++ {
		nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		buf[i] = chars[nBig.Int64()]
	}
	return string(buf)
}
