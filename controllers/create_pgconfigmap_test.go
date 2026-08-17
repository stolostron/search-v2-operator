// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestPostgresConfigmapRejectsInvalidWorkMem(t *testing.T) {
	// WORK_MEM containing shell/SQL metacharacters must not be embedded in postgresql-start.sh.
	malicious := `64MB'"; touch /tmp/pwned #`
	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec: searchv1alpha1.SearchSpec{
			Deployments: searchv1alpha1.SearchDeployments{
				Database: searchv1alpha1.DeploymentConfig{
					Env: []corev1.EnvVar{{Name: "WORK_MEM", Value: malicious}},
				},
			},
		},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	assert.NoError(t, err)

	objs := []runtime.Object{search}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	configMap := r.PostgresConfigmap(search)
	startScript := configMap.Data["postgresql-start.sh"]

	assert.NotContains(t, startScript, "touch /tmp/pwned",
		"untrusted WORK_MEM payload must not reach postgresql-start.sh")
	assert.Contains(t, startScript, "ALTER ROLE searchuser set work_mem='"+default_WORK_MEM+"'",
		"invalid WORK_MEM should fall back to the default")

	// Valid values are still accepted.
	for _, valid := range []string{"64MB", "4096kB", "1GB", "65536"} {
		assert.True(t, workMemPattern.MatchString(valid), "expected %q to be accepted", valid)
	}
}
