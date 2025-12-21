package config

import (
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Unset all XG2G vars to ensure clean test environment
	// We do this before running any tests in the package
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "XG2G_") {
			kv := strings.SplitN(e, "=", 2)
			if len(kv) > 0 {
				if err := os.Unsetenv(kv[0]); err != nil {
					// Hard to log here without t, but panicking is safe for a test setup failure
					panic("failed to unset env: " + err.Error())
				}
			}
		}
	}

	os.Exit(m.Run())
}
