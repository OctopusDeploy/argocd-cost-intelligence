package validation

import (
	"regexp"
	"strings"
)

// namespaceRegex matches valid Kubernetes namespace names (RFC 1123 DNS label).
// Must start and end with lowercase alphanumeric; hyphens allowed in the middle;
// maximum 63 characters total.
var namespaceRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// labelNameRegex matches the name (or prefix) portion of a Kubernetes label key.
var labelNameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9\-_.]*[a-zA-Z0-9])?$`)

// IsValidNamespace returns true when ns is a valid Kubernetes namespace name.
// This is enforced before any namespace value is interpolated into an OpenCost
// filter string to prevent filter injection.
func IsValidNamespace(ns string) bool {
	return namespaceRegex.MatchString(ns)
}

// IsValidLabelKey returns true for valid Kubernetes label keys, accepting both
// plain names (e.g. "team") and prefixed names (e.g. "app.kubernetes.io/name").
// Enforced before interpolating into OpenCost aggregate= parameters.
func IsValidLabelKey(key string) bool {
	if key == "" || len(key) > 316 {
		return false
	}
	parts := strings.SplitN(key, "/", 2)
	for _, part := range parts {
		if !labelNameRegex.MatchString(part) {
			return false
		}
	}
	return true
}
