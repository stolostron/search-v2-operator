// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	openshifttls "github.com/openshift/controller-runtime-common/pkg/tls"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var apiServerGVR = schema.GroupVersionResource{
	Group:    "config.openshift.io",
	Version:  "v1",
	Resource: "apiservers",
}

// getTLSEnvVars reads the cluster's APIServer TLS security profile and returns
// env vars for search components: TLS_MIN_VERSION (uint16 as string) and
// TLS_CIPHERS (comma-separated IANA names).
//
// Returns nil (no env vars) if the APIServer resource cannot be read, allowing
// components to use their built-in defaults.
func (r *SearchReconciler) getTLSEnvVars(ctx context.Context) []corev1.EnvVar {
	profileSpec, err := r.fetchTLSProfileSpec(ctx)
	if err != nil {
		log.Info("Could not read APIServer TLS profile, components will use defaults", "error", err)
		return nil
	}

	// Use controller-runtime-common to convert the profile to a tls.Config.
	// This handles OpenSSL→IANA→uint16 conversion via library-go — no hardcoded cipher maps.
	tlsConfigFn, unsupported := openshifttls.NewTLSConfigFromProfile(*profileSpec)
	if len(unsupported) > 0 {
		log.Info("Cipher suites not supported by Go, skipped", "ciphers", unsupported)
	}

	cfg := &tls.Config{}
	tlsConfigFn(cfg)

	// Convert uint16 cipher IDs back to IANA names for env var transport.
	cipherNames := cipherIDsToIANA(cfg.CipherSuites)

	log.V(2).Info("TLS profile resolved",
		"minVersion", cfg.MinVersion,
		"cipherCount", len(cipherNames),
	)

	return []corev1.EnvVar{
		newEnvVar("TLS_MIN_VERSION", strconv.FormatUint(uint64(cfg.MinVersion), 10)),
		newEnvVar("TLS_CIPHERS", strings.Join(cipherNames, ",")),
	}
}

// fetchTLSProfileSpec reads the APIServer resource and returns the resolved TLSProfileSpec.
func (r *SearchReconciler) fetchTLSProfileSpec(ctx context.Context) (*configv1.TLSProfileSpec, error) {
	obj, err := r.DynamicClient.Resource(apiServerGVR).Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get APIServer resource: %w", err)
	}

	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return defaultProfileSpec(), nil
	}

	profileRaw, exists := spec["tlsSecurityProfile"]
	if !exists || profileRaw == nil {
		return defaultProfileSpec(), nil
	}

	profileBytes, err := json.Marshal(profileRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal TLS profile: %w", err)
	}

	var profile configv1.TLSSecurityProfile
	if err := json.Unmarshal(profileBytes, &profile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal TLS profile: %w", err)
	}

	// Use controller-runtime-common's GetTLSProfileSpec to resolve the profile.
	// This handles preset types (Old, Intermediate, Modern) and Custom profiles.
	resolved, err := openshifttls.GetTLSProfileSpec(&profile)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve TLS profile: %w", err)
	}

	log.Info("Using cluster TLS profile", "type", profile.Type)
	return &resolved, nil
}

func defaultProfileSpec() *configv1.TLSProfileSpec {
	spec := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	return &spec
}

// cipherIDsToIANA converts crypto/tls cipher suite IDs to IANA names using Go's stdlib.
// No hardcoded map — automatically picks up new ciphers when Go adds them.
func cipherIDsToIANA(ids []uint16) []string {
	lookup := make(map[uint16]string)
	for _, cs := range tls.CipherSuites() {
		lookup[cs.ID] = cs.Name
	}
	for _, cs := range tls.InsecureCipherSuites() {
		lookup[cs.ID] = cs.Name
	}

	var names []string
	for _, id := range ids {
		if name, ok := lookup[id]; ok {
			names = append(names, name)
		}
	}
	return names
}
