// Copyright Contributors to the Open Cluster Management project
package imagevalidation

import (
	"fmt"
	"strings"
)

// TrustedImagePrefixes lists the registry prefixes that are permitted as image
// overrides. Any image that does not start with one of these prefixes is
// considered untrusted and must not be deployed.
var TrustedImagePrefixes = []string{
	"quay.io/stolostron/",
	"quay.io/acm-d/",
	"registry.redhat.io/",
}

// IsTrustedImage returns true when image originates from an approved registry.
func IsTrustedImage(image string) bool {
	for _, prefix := range TrustedImagePrefixes {
		if strings.HasPrefix(image, prefix) {
			return true
		}
	}
	return false
}

// ValidateImageRepo checks that image comes from a trusted registry.
// Returns an error if the image is empty or untrusted.
func ValidateImageRepo(image string) error {
	if image == "" {
		return fmt.Errorf("image reference must not be empty")
	}
	if !IsTrustedImage(image) {
		return fmt.Errorf("image %q is not from a trusted registry; allowed prefixes: %v", image, TrustedImagePrefixes)
	}
	return nil
}
