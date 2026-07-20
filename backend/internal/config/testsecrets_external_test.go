package config_test

import "testing"

// setRequiredTestSecrets mirrors the helper of the same name in the internal
// test package (testsecrets_test.go). Keep both in sync: a new mandatory
// secret is added in exactly these two files and nowhere else.
func setRequiredTestSecrets(tb testing.TB) {
	tb.Helper()
	tb.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
}
