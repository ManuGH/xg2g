package config

import "testing"

// setRequiredTestSecrets sets every mandatory secret the config validator
// requires, so tests can load a valid configuration without repeating
// literals. When a new mandatory key is introduced, add it HERE — not as
// inline t.Setenv calls across individual tests. A test that asserts the
// absence of a specific secret must unset that variable explicitly after
// calling this helper.
func setRequiredTestSecrets(tb testing.TB) {
	tb.Helper()
	tb.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
}
