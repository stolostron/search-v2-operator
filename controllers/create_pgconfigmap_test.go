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

func TestPostgresConfigmapWithStatementTimeout(t *testing.T) {

	search := &searchv1alpha1.Search{
		TypeMeta:   metav1.TypeMeta{Kind: "Search"},
		ObjectMeta: metav1.ObjectMeta{Name: "search-v2-operator", Namespace: "test-namespace"},
		Spec:       searchv1alpha1.SearchSpec{},
	}
	s := scheme.Scheme
	err := searchv1alpha1.SchemeBuilder.AddToScheme(s)
	assert.NoError(t, err)

	objs := []runtime.Object{search}
	cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()
	r := &SearchReconciler{Client: cl, Scheme: s}

	configMap := r.PostgresConfigmap(search)

	// Verify that statement_timeout is included in postgresql.conf
	postgresConf := configMap.Data["postgresql.conf"]
	assert.Contains(t, postgresConf, "statement_timeout = '60000")

	// Verify other expected settings are still present
	expectedSettings := []string{
		"ssl = 'on'",
		"ssl_cert_file = '/sslcert/tls.crt'",
		"ssl_key_file = '/sslcert/tls.key'",
		"max_parallel_workers_per_gather = '8'",
	}

	for _, setting := range expectedSettings {
		assert.Contains(t, postgresConf, setting)
	}
}

func TestUpdatePostgresConfigmapNewInstallation(t *testing.T) {
	// Test case: New installation - no existing custom config
	existing := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'",
			"custom-postgresql.conf": "",
		},
	}

	new := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'",
			"custom-postgresql.conf": "# Customizations appended to postgresql.conf.\n",
		},
	}

	UpdatePostgresConfigmap(existing, new)

	// Should use the new postgresql.conf with statement_timeout
	expectedConf := "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'"
	assert.Equal(t, expectedConf, new.Data["postgresql.conf"])
}

func TestUpdatePostgresConfigmapMigrationNeeded(t *testing.T) {
	// Test case: Migration needed - existing config has custom changes but no custom-postgresql.conf
	existing := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\ncustom_setting = 'custom_value'",
			"custom-postgresql.conf": "",
		},
	}

	new := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'",
			"custom-postgresql.conf": "# Customizations appended to postgresql.conf.\n",
		},
	}

	UpdatePostgresConfigmap(existing, new)

	// Should migrate custom setting to custom-postgresql.conf
	expectedCustomConf := "# Customizations appended to postgresql.conf\ncustom_setting = 'custom_value'"
	assert.Equal(t, expectedCustomConf, new.Data["custom-postgresql.conf"])

	// postgresql.conf should remain as the new default
	expectedConf := "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'"
	assert.Equal(t, expectedConf, new.Data["postgresql.conf"])
}

func TestUpdatePostgresConfigmapExistingCustomConfig(t *testing.T) {
	// Test case: Existing installation with custom-postgresql.conf
	existing := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'",
			"custom-postgresql.conf": "# My custom settings\nmax_connections = 200",
		},
	}

	new := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'",
			"custom-postgresql.conf": "# Customizations appended to postgresql.conf.\n",
		},
	}

	UpdatePostgresConfigmap(existing, new)

	// Should preserve existing custom config
	expectedCustomConf := "# My custom settings\nmax_connections = 200"
	assert.Equal(t, expectedCustomConf, new.Data["custom-postgresql.conf"])

	// Should merge custom config with new postgresql.conf
	expectedConf := "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'\n# My custom settings\nmax_connections = 200"
	assert.Equal(t, expectedConf, new.Data["postgresql.conf"])
}

func TestUpdatePostgresConfigmapCustomConfigAlreadyMerged(t *testing.T) {
	// Test case: Custom config already included in postgresql.conf
	customConfig := "# My custom settings\nmax_connections = 200"
	existing := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\n" + customConfig,
			"custom-postgresql.conf": customConfig,
		},
	}

	new := &corev1.ConfigMap{
		Data: map[string]string{
			"postgresql.conf":        "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'",
			"custom-postgresql.conf": "# Customizations appended to postgresql.conf.\n",
		},
	}

	UpdatePostgresConfigmap(existing, new)

	// Should preserve existing custom config
	assert.Equal(t, customConfig, new.Data["custom-postgresql.conf"])

	// Should merge custom config with new postgresql.conf
	expectedConf := "ssl = 'on'\nssl_cert_file = '/sslcert/tls.crt'\nstatement_timeout = '60000'\n" + customConfig
	assert.Equal(t, expectedConf, new.Data["postgresql.conf"])
}
