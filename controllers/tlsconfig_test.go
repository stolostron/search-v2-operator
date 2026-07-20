// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"crypto/tls"
	"strconv"
	"strings"
	"testing"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
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
