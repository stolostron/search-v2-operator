// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"strings"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInternalPGSecretUnchanged(t *testing.T) {
	instance := &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
	}
	r := &SearchReconciler{Scheme: testScheme()}

	secret := r.PGSecret(instance)

	assert.Equal(t, "search-postgres", secret.Name)
	assert.Equal(t, "searchuser", secret.StringData["database-user"])
	assert.Equal(t, DBNAME, secret.StringData["database-name"])
	assert.Len(t, strings.TrimSpace(secret.StringData["database-password"]), 16)
}
