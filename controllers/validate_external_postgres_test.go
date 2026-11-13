// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testWriterPassword = "wr1ter-p4ss" // #nosec G101 - test fixture, not a real credential
const testReaderPassword = "re4der-p4ss" // #nosec G101 - test fixture, not a real credential

// externalDBSecret builds a customer-provided Secret from the given keys.
func externalDBSecret(data map[string]string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-external-pg", Namespace: "test-namespace"},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{},
	}
	for k, v := range data {
		secret.Data[k] = []byte(v)
	}
	return secret
}

// validExternalDBData returns the minimum set of keys for a valid external DB Secret.
func validExternalDBData() map[string]string {
	return map[string]string{
		"db-url":          "postgresql://pg.example.com:5432/searchdb?sslmode=verify-full",
		"writer-user":     "search_writer",
		"writer-password": testWriterPassword,
		"reader-user":     "search_reader",
		"reader-password": testReaderPassword,
	}
}

func TestParseExternalDBSecretParsesAllFields(t *testing.T) {
	data := validExternalDBData()

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.NoError(t, err)
	assert.Equal(t, "pg.example.com", config.Host)
	assert.Equal(t, "5432", config.Port)
	assert.Equal(t, "searchdb", config.Database)
	assert.Equal(t, "verify-full", config.SSLMode)
	assert.Equal(t, "search_writer", config.WriterUser)
	assert.Equal(t, testWriterPassword, config.WriterPassword)
	assert.Equal(t, "search_reader", config.ReaderUser)
	assert.Equal(t, testReaderPassword, config.ReaderPassword)
}

func TestParseExternalDBSecretAppliesURLDefaults(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com/searchdb"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.NoError(t, err)
	assert.Equal(t, defaultExternalDBPort, config.Port)
	assert.Equal(t, defaultExternalDBSSLMode, config.SSLMode)
}

func TestParseExternalDBSecretAcceptsPostgresScheme(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgres://pg.example.com:6432/searchdb"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.NoError(t, err)
	assert.Equal(t, "6432", config.Port)
}

func TestParseExternalDBSecretReadsStringData(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-external-pg", Namespace: "test-namespace"},
		StringData: validExternalDBData(),
	}

	config, err := parseExternalDBSecret(secret)

	assert.NoError(t, err)
	assert.Equal(t, "search_writer", config.WriterUser)
}

// Secret values are used verbatim. Trimming credentials would silently change a
// password that legitimately contains leading or trailing whitespace, turning a
// valid Secret into an authentication failure the customer cannot explain.
func TestParseExternalDBSecretPreservesCredentialWhitespace(t *testing.T) {
	padded := "  " + testWriterPassword + "  "
	data := validExternalDBData()
	data["writer-password"] = padded

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.NoError(t, err)
	assert.Equal(t, padded, config.WriterPassword)
}

func TestParseExternalDBSecretRejectsNilSecret(t *testing.T) {
	config, err := parseExternalDBSecret(nil)

	assert.Nil(t, config)
	assert.Error(t, err)
}

func TestParseExternalDBSecretReportsAllMissingRequiredKeys(t *testing.T) {
	config, err := parseExternalDBSecret(externalDBSecret(map[string]string{}))

	assert.Nil(t, config)
	assert.Error(t, err)
	for _, key := range []string{"db-url", "writer-user", "writer-password", "reader-user", "reader-password"} {
		assert.Contains(t, err.Error(), key, "error should name every missing required key")
	}
}

func TestParseExternalDBSecretRejectsEmptyRequiredValue(t *testing.T) {
	data := validExternalDBData()
	data["reader-password"] = ""

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reader-password")
}

func TestParseExternalDBSecretRejectsCredentialsInURL(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://someuser:somepass@pg.example.com:5432/searchdb"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not contain credentials")
	assert.NotContains(t, err.Error(), "somepass", "error must not echo credentials")
	assert.NotContains(t, err.Error(), "someuser", "error must not echo credentials")
}

func TestParseExternalDBSecretRejectsNonPostgresScheme(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "mysql://pg.example.com:5432/searchdb"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scheme")
}

func TestParseExternalDBSecretRejectsUnparsableURL(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432/searchdb?sslmode=%zz"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "db-url")
}

func TestParseExternalDBSecretRejectsMissingHost(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql:///searchdb"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host")
}

func TestParseExternalDBSecretRejectsMissingDatabaseName(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database name")
}

func TestParseExternalDBSecretToleratesTrailingSlash(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432/searchdb/"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.NoError(t, err)
	assert.Equal(t, "searchdb", config.Database)
}

func TestParseExternalDBSecretRejectsExtraPathSegments(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432/searchdb/extra"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database name")
}

func TestParseExternalDBSecretRejectsInvalidPort(t *testing.T) {
	for _, port := range []string{"abc", "0", "70000", "-1"} {
		data := validExternalDBData()
		data["db-url"] = "postgresql://pg.example.com:" + port + "/searchdb"

		config, err := parseExternalDBSecret(externalDBSecret(data))

		assert.Nil(t, config, "port %q should be rejected", port)
		assert.Error(t, err, "port %q should be rejected", port)
	}
}

func TestParseExternalDBSecretRejectsInvalidSSLMode(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432/searchdb?sslmode=bogus"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "sslmode")
}

// Unsupported query parameters are rejected rather than ignored: silently dropping
// something like connect_timeout would leave the customer believing it took effect.
func TestParseExternalDBSecretRejectsUnsupportedQueryParameters(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432/searchdb?sslmode=require&connect_timeout=10&application_name=x"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "only supports the sslmode query parameter")
}

func TestParseExternalDBSecretRejectsURLFragment(t *testing.T) {
	data := validExternalDBData()
	data["db-url"] = "postgresql://pg.example.com:5432/searchdb#fragment"

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "fragment")
}

// The operator must never mount a customer-owned secret through CR env vars.
// operatorManagedSecrets stays limited to secrets the operator itself creates.
func TestExternalDBSecretIsNotOperatorManaged(t *testing.T) {
	_, ok := operatorManagedSecrets["my-external-pg"]
	assert.False(t, ok)
}
