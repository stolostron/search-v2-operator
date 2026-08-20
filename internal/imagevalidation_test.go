// Copyright Contributors to the Open Cluster Management project
package imagevalidation

import "testing"

func TestIsTrustedImage(t *testing.T) {
	cases := []struct {
		image   string
		trusted bool
	}{
		{"quay.io/stolostron/search-api:2.7.0", true},
		{"quay.io/stolostron/search-indexer:latest", true},
		{"quay.io/acm-d/search-collector:2.7.0", true},
		{"registry.redhat.io/rhacm2/search-postgres:2.7.0", true},
		{"quay.io/test/evil:latest", false},
		{"docker.io/library/ubuntu:latest", false},
		{"gcr.io/google-containers/pause:3.1", false},
		// Ensure prefix matching is not fooled by lookalike hostnames
		{"quay.io/stolostron.evil.com/image:tag", false},
		{"", false},
	}
	for _, c := range cases {
		got := IsTrustedImage(c.image)
		if got != c.trusted {
			t.Errorf("IsTrustedImage(%q) = %v, want %v", c.image, got, c.trusted)
		}
	}
}

func TestValidateImageRepo(t *testing.T) {
	cases := []struct {
		image   string
		wantErr bool
	}{
		{"quay.io/stolostron/search-api:2.7.0", false},
		{"quay.io/acm-d/search-collector:2.7.0", false},
		{"registry.redhat.io/rhacm2/search-postgres:2.7.0", false},
		{"quay.io/test/evil:latest", true},
		{"", true},
	}
	for _, c := range cases {
		err := ValidateImageRepo(c.image)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateImageRepo(%q) error = %v, wantErr %v", c.image, err, c.wantErr)
		}
	}
}
