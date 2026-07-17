package profiles

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestProfileResolutionReadsEnvironmentOnlyAtConfigBoundary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "config_snapshot.go" {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range [][]byte{[]byte("config.Parse"), []byte("os.Getenv"), []byte("os.LookupEnv")} {
			if bytes.Contains(content, forbidden) {
				t.Errorf("%s reads process environment outside config_snapshot.go via %q", name, forbidden)
			}
		}
	}
}
