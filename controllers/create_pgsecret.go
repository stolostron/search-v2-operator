// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"crypto/rand"
	"math/big"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const DBNAME = "search"

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
		"database-name":     DBNAME,
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
