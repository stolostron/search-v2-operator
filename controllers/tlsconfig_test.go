// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"crypto/tls"
	"strconv"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeAPIServer(tlsProfile map[string]interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "APIServer",
			"metadata": map[string]interface{}{
				"name": "cluster",
			},
			"spec": map[string]interface{}{},
		},
	}
	if tlsProfile != nil {
		obj.Object["spec"].(map[string]interface{})["tlsSecurityProfile"] = tlsProfile
	}
	return obj
}

func newFakeDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "APIServer"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "APIServerList"},
		&unstructured.UnstructuredList{},
	)
	return dynamicfake.NewSimpleDynamicClient(scheme, objects...)
}

func TestCipherIDsToIANA(t *testing.T) {
	ids := []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384}
	names := cipherIDsToIANA(ids)

	assert.Equal(t, []string{
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
	}, names)
}

func TestCipherIDsToIANA_Empty(t *testing.T) {
	names := cipherIDsToIANA(nil)
	assert.Nil(t, names)
}

func TestGetTLSEnvVars_NoAPIServer(t *testing.T) {
	client := newFakeDynamicClient() // no objects
	r := &SearchReconciler{DynamicClient: client}

	envVars := r.getTLSEnvVars(context.Background())
	assert.Nil(t, envVars, "Should return nil when APIServer cannot be read")
}

func TestGetTLSEnvVars_NoProfile(t *testing.T) {
	apiServer := newFakeAPIServer(nil)
	client := newFakeDynamicClient(apiServer)
	r := &SearchReconciler{DynamicClient: client}

	envVars := r.getTLSEnvVars(context.Background())

	assert.Len(t, envVars, 2)
	assert.Equal(t, "TLS_MIN_VERSION", envVars[0].Name)
	assert.Equal(t, strconv.FormatUint(uint64(tls.VersionTLS12), 10), envVars[0].Value)
	assert.Equal(t, "TLS_CIPHERS", envVars[1].Name)
	assert.NotEmpty(t, envVars[1].Value)
}

func TestGetTLSEnvVars_IntermediateProfile(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{
		"type": "Intermediate",
	})
	client := newFakeDynamicClient(apiServer)
	r := &SearchReconciler{DynamicClient: client}

	envVars := r.getTLSEnvVars(context.Background())

	assert.Len(t, envVars, 2)
	assert.Equal(t, strconv.FormatUint(uint64(tls.VersionTLS12), 10), envVars[0].Value)
}

func TestGetTLSEnvVars_OldProfile(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{
		"type": "Old",
	})
	client := newFakeDynamicClient(apiServer)
	r := &SearchReconciler{DynamicClient: client}

	envVars := r.getTLSEnvVars(context.Background())

	assert.Len(t, envVars, 2)
	assert.Equal(t, strconv.FormatUint(uint64(tls.VersionTLS10), 10), envVars[0].Value)
}

func TestGetTLSEnvVars_CustomProfile(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{
		"type": "Custom",
		"custom": map[string]interface{}{
			"ciphers":       []interface{}{"ECDHE-RSA-AES256-GCM-SHA384"},
			"minTLSVersion": "VersionTLS13",
		},
	})
	client := newFakeDynamicClient(apiServer)
	r := &SearchReconciler{DynamicClient: client}

	envVars := r.getTLSEnvVars(context.Background())

	assert.Len(t, envVars, 2)
	assert.Equal(t, strconv.FormatUint(uint64(tls.VersionTLS13), 10), envVars[0].Value)
	// With TLS 1.3, NewTLSConfigFromProfile skips CipherSuites, so TLS_CIPHERS may be empty.
	// The cipher is still valid — just not set because Go auto-manages TLS 1.3 ciphers.
}

func TestGetTLSEnvVars_CiphersAreIANANames(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{
		"type": "Intermediate",
	})
	client := newFakeDynamicClient(apiServer)
	r := &SearchReconciler{DynamicClient: client}

	envVars := r.getTLSEnvVars(context.Background())

	ciphers := envVars[1].Value
	// All cipher names should be IANA format (TLS_ prefix)
	for _, name := range strings.Split(ciphers, ",") {
		assert.True(t, strings.HasPrefix(name, "TLS_"),
			"Cipher %q should be in IANA format (TLS_ prefix)", name)
	}
}

func TestIndexerDeploymentIncludesTLSEnvVars(t *testing.T) {
	instance := newTLSTestSearchInstance()

	s := runtime.NewScheme()
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().WithScheme(s).Build(),
		DynamicClient: newFakeDynamicClient(),
		Scheme:        s,
	}

	tlsEnvVars := []corev1.EnvVar{
		{Name: "TLS_MIN_VERSION", Value: "771"},
		{Name: "TLS_CIPHERS", Value: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
	}

	deployment := r.IndexerDeployment(instance, tlsEnvVars)
	envNames := envVarNames(deployment.Spec.Template.Spec.Containers[0].Env)
	assert.Contains(t, envNames, "TLS_MIN_VERSION")
	assert.Contains(t, envNames, "TLS_CIPHERS")
}

func TestAPIDeploymentIncludesTLSEnvVars(t *testing.T) {
	instance := newTLSTestSearchInstance()

	s := runtime.NewScheme()
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().WithScheme(s).Build(),
		DynamicClient: newFakeDynamicClient(),
		Scheme:        s,
	}

	tlsEnvVars := []corev1.EnvVar{
		{Name: "TLS_MIN_VERSION", Value: "771"},
		{Name: "TLS_CIPHERS", Value: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
	}

	deployment := r.APIDeployment(instance, tlsEnvVars)
	envNames := envVarNames(deployment.Spec.Template.Spec.Containers[0].Env)
	assert.Contains(t, envNames, "TLS_MIN_VERSION")
	assert.Contains(t, envNames, "TLS_CIPHERS")
}

func TestDeploymentsWorkWithNilTLSEnvVars(t *testing.T) {
	instance := newTLSTestSearchInstance()

	s := runtime.NewScheme()
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().WithScheme(s).Build(),
		DynamicClient: newFakeDynamicClient(),
		Scheme:        s,
	}

	indexer := r.IndexerDeployment(instance, nil)
	api := r.APIDeployment(instance, nil)

	indexerNames := envVarNames(indexer.Spec.Template.Spec.Containers[0].Env)
	apiNames := envVarNames(api.Spec.Template.Spec.Containers[0].Env)

	assert.NotContains(t, indexerNames, "TLS_MIN_VERSION")
	assert.NotContains(t, apiNames, "TLS_MIN_VERSION")
}

func TestTlsVersionToPostgres(t *testing.T) {
	tests := []struct {
		input configv1.TLSProtocolVersion
		want  string
	}{
		{configv1.VersionTLS10, "TLSv1"},
		{configv1.VersionTLS11, "TLSv1.1"},
		{configv1.VersionTLS12, "TLSv1.2"},
		{configv1.VersionTLS13, "TLSv1.3"},
		{"UnknownVersion", "TLSv1.2"},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			assert.Equal(t, tt.want, tlsVersionToPostgres(tt.input))
		})
	}
}

func TestOpensslCiphersFromProfile(t *testing.T) {
	t.Run("filters TLS 1.3 ciphers", func(t *testing.T) {
		ciphers := []string{
			"TLS_AES_128_GCM_SHA256",
			"ECDHE-RSA-AES128-GCM-SHA256",
			"ECDHE-RSA-AES256-GCM-SHA384",
		}
		result := opensslCiphersFromProfile(ciphers)
		assert.Equal(t, "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384", result)
	})

	t.Run("all TLS 1.3 returns fallback", func(t *testing.T) {
		assert.Equal(t, "HIGH:!aNULL", opensslCiphersFromProfile(
			[]string{"TLS_AES_128_GCM_SHA256", "TLS_CHACHA20_POLY1305_SHA256"}))
	})

	t.Run("nil returns fallback", func(t *testing.T) {
		assert.Equal(t, "HIGH:!aNULL", opensslCiphersFromProfile(nil))
	})

	t.Run("empty returns fallback", func(t *testing.T) {
		assert.Equal(t, "HIGH:!aNULL", opensslCiphersFromProfile([]string{}))
	})

	t.Run("no TLS 1.3 passes all through", func(t *testing.T) {
		ciphers := []string{"ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"}
		assert.Equal(t, "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384",
			opensslCiphersFromProfile(ciphers))
	})
}

func TestGetPostgresTLSConfig_Intermediate(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{"type": "Intermediate"})
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().Build(),
		Scheme:        scheme.Scheme,
		DynamicClient: newFakeDynamicClient(apiServer),
	}

	cfg := r.getPostgresTLSConfig(context.TODO())

	assert.Equal(t, "TLSv1.2", cfg.SSLMinProtocolVersion)
	assert.NotEmpty(t, cfg.SSLCiphers)
	assert.NotContains(t, cfg.SSLCiphers, "TLS_")
}

func TestGetPostgresTLSConfig_Old(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{"type": "Old"})
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().Build(),
		Scheme:        scheme.Scheme,
		DynamicClient: newFakeDynamicClient(apiServer),
	}

	cfg := r.getPostgresTLSConfig(context.TODO())

	assert.Equal(t, "TLSv1", cfg.SSLMinProtocolVersion)
	assert.Contains(t, cfg.SSLCiphers, "DES-CBC3-SHA")
}

func TestGetPostgresTLSConfig_Modern(t *testing.T) {
	apiServer := newFakeAPIServer(map[string]interface{}{"type": "Modern"})
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().Build(),
		Scheme:        scheme.Scheme,
		DynamicClient: newFakeDynamicClient(apiServer),
	}

	cfg := r.getPostgresTLSConfig(context.TODO())

	assert.Equal(t, "TLSv1.3", cfg.SSLMinProtocolVersion)
	assert.Equal(t, "HIGH:!aNULL", cfg.SSLCiphers)
}

func TestGetPostgresTLSConfig_NoAPIServer(t *testing.T) {
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().Build(),
		Scheme:        scheme.Scheme,
		DynamicClient: newFakeDynamicClient(),
	}

	cfg := r.getPostgresTLSConfig(context.TODO())

	assert.Equal(t, "TLSv1.2", cfg.SSLMinProtocolVersion)
	assert.NotEmpty(t, cfg.SSLCiphers)
}

func TestGetPostgresTLSConfig_NoProfile(t *testing.T) {
	apiServer := newFakeAPIServer(nil)
	r := &SearchReconciler{
		Client:        fake.NewClientBuilder().Build(),
		Scheme:        scheme.Scheme,
		DynamicClient: newFakeDynamicClient(apiServer),
	}

	cfg := r.getPostgresTLSConfig(context.TODO())

	assert.Equal(t, "TLSv1.2", cfg.SSLMinProtocolVersion)
	assert.NotEmpty(t, cfg.SSLCiphers)
}

func TestPostgresConfigmapTLSSettings(t *testing.T) {
	r := &SearchReconciler{
		Client: fake.NewClientBuilder().Build(),
		Scheme: scheme.Scheme,
	}
	cm := r.PostgresConfigmap(newTLSTestSearchInstance(), PostgresTLSConfig{
		SSLMinProtocolVersion: "TLSv1.3",
		SSLCiphers:            "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384",
	})

	conf := cm.Data["postgresql.conf"]
	assert.Contains(t, conf, "ssl_min_protocol_version = 'TLSv1.3'")
	assert.Contains(t, conf, "ssl_ciphers = 'ECDHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384'")
}

func TestPostgresConfigHash_ChangesWithConfig(t *testing.T) {
	hash1 := postgresConfigHash(map[string]string{
		"postgresql.conf": "ssl_min_protocol_version = 'TLSv1.2'",
	})
	hash2 := postgresConfigHash(map[string]string{
		"postgresql.conf": "ssl_min_protocol_version = 'TLSv1.3'",
	})

	assert.NotEqual(t, hash1, hash2)
}

func TestPostgresConfigHash_Stable(t *testing.T) {
	data := map[string]string{"postgresql.conf": "ssl = 'on'"}
	assert.Equal(t, postgresConfigHash(data), postgresConfigHash(data))
}

func TestPostgresConfigHash_IgnoresScripts(t *testing.T) {
	base := map[string]string{
		"postgresql.conf":     "ssl = 'on'",
		"postgresql-start.sh": "echo v1",
	}
	modified := map[string]string{
		"postgresql.conf":     "ssl = 'on'",
		"postgresql-start.sh": "echo v2",
	}
	assert.Equal(t, postgresConfigHash(base), postgresConfigHash(modified))
}

func TestPGDeployment_ConfigHashAnnotation(t *testing.T) {
	r := &SearchReconciler{
		Client: fake.NewClientBuilder().Build(),
		Scheme: scheme.Scheme,
	}

	dep := r.PGDeployment(newTLSTestSearchInstance(), "abc123")

	require.NotNil(t, dep.Spec.Template.Annotations)
	assert.Equal(t, "abc123",
		dep.Spec.Template.Annotations["search.open-cluster-management.io/postgres-config-hash"])
}

// helpers

func newTLSTestSearchInstance() *searchv1alpha1.Search {
	return &searchv1alpha1.Search{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "search-v2-operator",
			Namespace: "open-cluster-management",
		},
		Spec: searchv1alpha1.SearchSpec{},
	}
}

func envVarNames(envs []corev1.EnvVar) []string {
	names := make([]string, len(envs))
	for i, e := range envs {
		names[i] = e.Name
	}
	return names
}
