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
		"database-host":   "pg.example.com",
		"database-port":   "5432",
		"database-name":   "searchdb",
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
	assert.Equal(t, "search_writer", config.WriterUser)
	assert.Equal(t, testWriterPassword, config.WriterPassword)
	assert.Equal(t, "search_reader", config.ReaderUser)
	assert.Equal(t, testReaderPassword, config.ReaderPassword)
}

// The default applies only when the key is absent.
func TestParseExternalDBSecretDefaultsPortWhenKeyAbsent(t *testing.T) {
	data := validExternalDBData()
	delete(data, "database-port")

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.NoError(t, err)
	assert.Equal(t, defaultExternalDBPort, config.Port)
}

func TestParseExternalDBSecretDefaultsPortWhenKeyAbsentFromStringData(t *testing.T) {
	data := validExternalDBData()
	delete(data, "database-port")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-external-pg", Namespace: "test-namespace"},
		StringData: data,
	}

	config, err := parseExternalDBSecret(secret)

	assert.NoError(t, err)
	assert.Equal(t, defaultExternalDBPort, config.Port)
}

// A present-but-empty database-port is a mistake, not a request for the default:
// silently falling back to 5432 would hide a broken Secret.
func TestParseExternalDBSecretRejectsEmptyPort(t *testing.T) {
	data := validExternalDBData()
	data["database-port"] = ""

	config, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database-port")
}

// Presence is resolved against StringData too when Data does not carry the key.
func TestParseExternalDBSecretRejectsEmptyPortInStringData(t *testing.T) {
	data := validExternalDBData()
	data["database-port"] = ""
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-external-pg", Namespace: "test-namespace"},
		StringData: data,
	}

	config, err := parseExternalDBSecret(secret)

	assert.Nil(t, config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database-port")
}

// Data wins over StringData for the same key, including for presence checks.
func TestParseExternalDBSecretPrefersDataOverStringData(t *testing.T) {
	secret := externalDBSecret(validExternalDBData())
	secret.Data["database-port"] = []byte("6432")
	secret.StringData = map[string]string{"database-port": "5433"}

	config, err := parseExternalDBSecret(secret)

	assert.NoError(t, err)
	assert.Equal(t, "6432", config.Port)
}

func TestParseExternalDBSecretAcceptsNonDefaultPort(t *testing.T) {
	data := validExternalDBData()
	data["database-port"] = "6432"

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
	for _, key := range []string{
		"database-host", "database-name",
		"writer-user", "writer-password",
		"reader-user", "reader-password",
	} {
		assert.Contains(t, err.Error(), key, "error should name every missing required key")
	}
	assert.NotContains(t, err.Error(), "database-port", "database-port is optional")
}

func TestParseExternalDBSecretRejectsEmptyRequiredValue(t *testing.T) {
	for _, key := range []string{"database-host", "database-name", "reader-password"} {
		data := validExternalDBData()
		data[key] = ""

		config, err := parseExternalDBSecret(externalDBSecret(data))

		assert.Nil(t, config, "empty %q should be rejected", key)
		assert.Error(t, err, "empty %q should be rejected", key)
		assert.Contains(t, err.Error(), key)
	}
}

func TestParseExternalDBSecretRejectsInvalidPort(t *testing.T) {
	for _, port := range []string{"abc", "0", "70000", "-1", "54 32", "5432.0"} {
		data := validExternalDBData()
		data["database-port"] = port

		config, err := parseExternalDBSecret(externalDBSecret(data))

		assert.Nil(t, config, "port %q should be rejected", port)
		assert.Error(t, err, "port %q should be rejected", port)
		assert.Contains(t, err.Error(), "database-port")
		assert.NotContains(t, err.Error(), port, "error must not echo the offending value")
	}
}

// Errors are surfaced in logs and CR status, so they name keys only.
func TestParseExternalDBSecretErrorsDoNotEchoCredentials(t *testing.T) {
	data := validExternalDBData()
	data["database-port"] = "notaport"
	data["writer-password"] = "sup3r-s3cret" // #nosec G101 - test fixture, not a real credential

	_, err := parseExternalDBSecret(externalDBSecret(data))

	assert.Error(t, err)
	assert.NotContains(t, err.Error(), "sup3r-s3cret")
}

// The operator must never mount a customer-owned secret through CR env vars.
// operatorManagedSecrets stays limited to secrets the operator itself creates.
func TestExternalDBSecretIsNotOperatorManaged(t *testing.T) {
	_, ok := operatorManagedSecrets["my-external-pg"]
	assert.False(t, ok)
}
