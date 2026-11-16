// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Keys of the customer-provided external PostgreSQL Secret referenced by
// Search.spec.externalDBInstance. The Secret is owned by the customer: the
// operator only reads it and never creates, mutates, or takes ownership of it.
const (
	// The endpoint is shared by both roles: search-indexer and search-v2-api
	// connect to the same host, port and database with different credentials.
	// A separate reader endpoint is handled in a later PR.
	externalDBHostKey = "database-host" // Required.
	externalDBPortKey = "database-port" // Optional, defaults to 5432.
	externalDBNameKey = "database-name" // Required.

	// Writer credentials are used by search-indexer. Required.
	externalDBWriterUserKey     = "writer-user"
	externalDBWriterPasswordKey = "writer-password" // #nosec G101 - this is a secret key name, not a password

	// Reader credentials are used by search-v2-api. Required.
	externalDBReaderUserKey     = "reader-user"
	externalDBReaderPasswordKey = "reader-password" // #nosec G101 - this is a secret key name, not a password
)

const defaultExternalDBPort = "5432"

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

	WriterUser     string
	WriterPassword string

	ReaderUser     string
	ReaderPassword string
}

// externalDBSecretLookup reads a key from the Secret, preferring Data and falling
// back to StringData, and reports whether the key is present at all. Values are
// returned verbatim: trimming would silently alter credentials that legitimately
// contain leading or trailing whitespace.
//
// Presence is distinct from emptiness for optional keys: an absent key means
// "use the default", while a key the customer set to an empty value is a mistake
// worth reporting rather than papering over.
func externalDBSecretLookup(secret *corev1.Secret, key string) (string, bool) {
	if v, ok := secret.Data[key]; ok {
		return string(v), true
	}
	v, ok := secret.StringData[key]
	return v, ok
}

// externalDBSecretValue returns the value of a key, or "" when it is absent.
func externalDBSecretValue(secret *corev1.Secret, key string) string {
	v, _ := externalDBSecretLookup(secret, key)
	return v
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

	port, portSet := externalDBSecretLookup(secret, externalDBPortKey)

	config := &ExternalDBConfig{
		Host:           externalDBSecretValue(secret, externalDBHostKey),
		Port:           port,
		Database:       externalDBSecretValue(secret, externalDBNameKey),
		WriterUser:     externalDBSecretValue(secret, externalDBWriterUserKey),
		WriterPassword: externalDBSecretValue(secret, externalDBWriterPasswordKey),
		ReaderUser:     externalDBSecretValue(secret, externalDBReaderUserKey),
		ReaderPassword: externalDBSecretValue(secret, externalDBReaderPasswordKey),
	}

	// Report every missing required key at once so the customer can fix the
	// Secret in a single edit.
	var missing []string
	for _, required := range []struct {
		key   string
		value string
	}{
		{externalDBHostKey, config.Host},
		{externalDBNameKey, config.Database},
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

	// Omitting the key is the common case; a key the customer did set must be a
	// usable TCP port, otherwise every connection attempt would fail later with
	// an error far from its cause. An explicitly empty value is a mistake, not a
	// request for the default.
	if !portSet {
		config.Port = defaultExternalDBPort
	} else {
		portNumber, err := strconv.Atoi(config.Port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("external database secret %q has an invalid %q: must be a number between 1 and 65535",
				secret.GetName(), externalDBPortKey)
		}
	}

	return config, nil
}
