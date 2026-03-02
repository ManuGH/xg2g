package auth

// testDecisionSecret is a well-known key used ONLY by tests.
// It is never used in production; the server requires an explicit secret at startup.
//
//nolint:gosec // Test-only secret; never used in production paths.
var testDecisionSecret = []byte("xg2g-test-decision-secret-32byte")

// TestSecret returns a copy of the test-only signing key.
// Callers receive an independent slice to prevent shared-backing-array mutations.
func TestSecret() []byte {
	out := make([]byte, len(testDecisionSecret))
	copy(out, testDecisionSecret)
	return out
}
