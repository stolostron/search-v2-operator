// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const DBNAME = "search"

const (
	apiReadonlySecretName = "search-postgres-api-readonly" // #nosec G101 - False positive, this is a secret name, not a password
	mcpReadonlySecretName = "search-postgres-mcp-readonly" // #nosec G101 - False positive, this is a secret name, not a password
)

// Keys of the customer-provided external PostgreSQL Secret referenced by
// Search.spec.externalDBInstance. The Secret is owned by the customer: the
// operator only reads it and never creates, mutates, or takes ownership of it.
const (
	// externalDBURLKey holds the connection endpoint, for example
	// "postgresql://pg.example.com:5432/searchdb?sslmode=verify-full".
	// Credentials must NOT be embedded in this URL; they are supplied per role below.
	externalDBURLKey = "db-url"

	// Writer credentials are used by search-indexer. Required.
	externalDBWriterUserKey     = "writer-user"
	externalDBWriterPasswordKey = "writer-password" // #nosec G101 - this is a secret key name, not a password

	// Reader credentials are used by search-v2-api. Required.
	externalDBReaderUserKey     = "reader-user"
	externalDBReaderPasswordKey = "reader-password" // #nosec G101 - this is a secret key name, not a password

	// MCP credentials are optional. When supplied, both keys must be present.
	externalDBMCPUserKey     = "mcp-user"
	externalDBMCPPasswordKey = "mcp-password" // #nosec G101 - this is a secret key name, not a password
)

const (
	// externalDBSSLModeParam is the only query parameter supported in db-url.
	externalDBSSLModeParam = "sslmode"

	defaultExternalDBPort    = "5432"
	defaultExternalDBSSLMode = "require"
)

// validExternalDBSSLModes are the sslmode values accepted by libpq.
var validExternalDBSSLModes = map[string]struct{}{
	"disable":     {},
	"allow":       {},
	"prefer":      {},
	"require":     {},
	"verify-ca":   {},
	"verify-full": {},
}

// ExternalDBConfig is the validated content of the customer-provided external
// PostgreSQL Secret.
//
// It holds plaintext credentials. Never log this struct or any of its password
// fields, and never format it with %v or %+v; log individual non-credential
// fields such as Host, Port and Database instead.
type ExternalDBConfig struct {
	Host     string
	Port     string
	Database string
	SSLMode  string

	WriterUser     string
	WriterPassword string

	ReaderUser     string
	ReaderPassword string

	// MCPUser and MCPPassword are empty when the customer did not supply them.
	MCPUser     string
	MCPPassword string
}

// externalDBSecretValue reads a key from the Secret, preferring Data and falling
// back to StringData. Values are returned verbatim: trimming would silently alter
// credentials that legitimately contain leading or trailing whitespace.
func externalDBSecretValue(secret *corev1.Secret, key string) string {
	if v, ok := secret.Data[key]; ok {
		return string(v)
	}
	return secret.StringData[key]
}

// parseExternalDBSecret validates a customer-provided external PostgreSQL Secret
// and returns its contents. Errors name only the offending Secret keys, never
// their values, so they are safe to surface in logs and CR status.
//
// This performs structural validation only. Connecting to the database and
// checking the server version (PostgreSQL >= 16) is handled in a later PR.
func parseExternalDBSecret(secret *corev1.Secret) (*ExternalDBConfig, error) {
	if secret == nil {
		return nil, fmt.Errorf("external database secret is nil")
	}

	config := &ExternalDBConfig{
		WriterUser:     externalDBSecretValue(secret, externalDBWriterUserKey),
		WriterPassword: externalDBSecretValue(secret, externalDBWriterPasswordKey),
		ReaderUser:     externalDBSecretValue(secret, externalDBReaderUserKey),
		ReaderPassword: externalDBSecretValue(secret, externalDBReaderPasswordKey),
		MCPUser:        externalDBSecretValue(secret, externalDBMCPUserKey),
		MCPPassword:    externalDBSecretValue(secret, externalDBMCPPasswordKey),
	}

	dbURL := externalDBSecretValue(secret, externalDBURLKey)

	// Report every missing required key at once so the customer can fix the
	// Secret in a single edit.
	var missing []string
	for _, required := range []struct {
		key   string
		value string
	}{
		{externalDBURLKey, dbURL},
		{externalDBWriterUserKey, config.WriterUser},
		{externalDBWriterPasswordKey, config.WriterPassword},
		{externalDBReaderUserKey, config.ReaderUser},
		{externalDBReaderPasswordKey, config.ReaderPassword},
	} {
		if required.value == "" {
			missing = append(missing, required.key)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("external database secret %q is missing required key(s): %s",
			secret.GetName(), strings.Join(missing, ", "))
	}

	// MCP credentials are optional, but must be supplied as a pair.
	if config.MCPUser != "" && config.MCPPassword == "" {
		return nil, fmt.Errorf("external database secret %q sets %q without %q; supply both or neither",
			secret.GetName(), externalDBMCPUserKey, externalDBMCPPasswordKey)
	}
	if config.MCPPassword != "" && config.MCPUser == "" {
		return nil, fmt.Errorf("external database secret %q sets %q without %q; supply both or neither",
			secret.GetName(), externalDBMCPPasswordKey, externalDBMCPUserKey)
	}

	if err := parseExternalDBURL(dbURL, config); err != nil {
		return nil, fmt.Errorf("external database secret %q has an invalid %q: %w",
			secret.GetName(), externalDBURLKey, err)
	}

	return config, nil
}

// parseExternalDBURL validates the db-url value and fills the endpoint fields of config.
func parseExternalDBURL(dbURL string, config *ExternalDBConfig) error {
	parsed, err := url.Parse(dbURL)
	if err != nil {
		// err wraps the URL, which could carry credentials if the customer
		// embedded them, so report only that parsing failed.
		return fmt.Errorf("value is not a valid URL")
	}

	if parsed.Scheme != "postgresql" && parsed.Scheme != "postgres" {
		return fmt.Errorf("scheme must be postgresql:// or postgres://")
	}

	// Credentials belong in the per-role keys. Accepting them here would create
	// a second, ambiguous source of truth.
	if parsed.User != nil {
		return fmt.Errorf("URL must not contain credentials; use the %q, %q, %q and %q keys instead",
			externalDBWriterUserKey, externalDBWriterPasswordKey, externalDBReaderUserKey, externalDBReaderPasswordKey)
	}

	config.Host = parsed.Hostname()
	if config.Host == "" {
		return fmt.Errorf("URL must specify a host")
	}

	config.Port = parsed.Port()
	if config.Port == "" {
		config.Port = defaultExternalDBPort
	} else {
		port, err := strconv.Atoi(config.Port)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("port must be a number between 1 and 65535")
		}
	}

	config.Database = strings.TrimPrefix(strings.TrimSuffix(parsed.Path, "/"), "/")
	if config.Database == "" || strings.Contains(config.Database, "/") {
		return fmt.Errorf("URL path must be a database name, for example postgresql://host:5432/searchdb")
	}

	if parsed.Fragment != "" {
		return fmt.Errorf("URL must not contain a fragment")
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return fmt.Errorf("query parameters are not valid")
	}

	// Reject anything we do not act on. Ignoring an unrecognized parameter would
	// leave the customer believing it took effect.
	for name := range query {
		if name != externalDBSSLModeParam {
			return fmt.Errorf("db-url only supports the sslmode query parameter")
		}
	}

	config.SSLMode = query.Get(externalDBSSLModeParam)
	if config.SSLMode == "" {
		config.SSLMode = defaultExternalDBSSLMode
	}
	if _, ok := validExternalDBSSLModes[config.SSLMode]; !ok {
		return fmt.Errorf("unsupported sslmode")
	}

	return nil
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
		"database-name":     DBNAME,
	}

	err := controllerutil.SetControllerReference(instance, secret, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres secret")
	}
	return secret
}

func (r *SearchReconciler) APIReadonlySecret(instance *searchv1alpha1.Search) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      apiReadonlySecretName,
			Namespace: instance.GetNamespace(),
		},
		Type: corev1.SecretTypeOpaque,
	}
	secret.StringData = map[string]string{
		"database-user":     "search_api_ro",
		"database-password": generatePass(16),
		"database-name":     DBNAME,
	}
	err := controllerutil.SetControllerReference(instance, secret, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres-api-readonly secret")
	}
	return secret
}

func (r *SearchReconciler) MCPReadonlySecret(instance *searchv1alpha1.Search) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpReadonlySecretName,
			Namespace: instance.GetNamespace(),
		},
		Type: corev1.SecretTypeOpaque,
	}
	secret.StringData = map[string]string{
		"database-user":     "search_mcp_ro",
		"database-password": generatePass(16),
		"database-name":     DBNAME,
	}
	err := controllerutil.SetControllerReference(instance, secret, r.Scheme)
	if err != nil {
		log.V(2).Info("Could not set control for search-postgres-mcp-readonly secret")
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
