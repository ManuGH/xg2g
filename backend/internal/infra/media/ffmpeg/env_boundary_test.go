package ffmpeg

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestFFmpegPlanningReadsEnvironmentOnlyAtConfigBoundary(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "adapter_config.go" {
			continue
		}
		content, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range [][]byte{[]byte("config.Parse"), []byte("os.Getenv"), []byte("os.LookupEnv")} {
			if bytes.Contains(content, forbidden) {
				t.Errorf("%s reads process environment outside adapter_config.go via %q", name, forbidden)
			}
		}
	}
}
