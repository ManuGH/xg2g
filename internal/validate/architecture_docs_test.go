package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureDoc_NoStaleLayeringClaims prevents reintroducing resolved
// architecture violations into the canonical architecture documentation.
func TestArchitectureDoc_NoStaleLayeringClaims(t *testing.T) {
	projectRoot := findProjectRoot(t)
	docPath := filepath.Join(projectRoot, "docs", "arch", "ARCHITECTURE.md")

	raw, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read architecture doc: %v", err)
	}
	doc := string(raw)

	forbidden := []string{
		"`infra/ffmpeg/builder.go` → `control/vod` ❌",
		"`infra/ffmpeg/probe.go` → `control/vod` ❌",
		"`infra/ffmpeg/runner.go` → `control/vod` ❌",
		"| `core/` migration | Incremental (non-blocking) | Team | Ongoing |",
	}
	for _, snippet := range forbidden {
		if strings.Contains(doc, snippet) {
			t.Fatalf("stale architecture claim found in docs/arch/ARCHITECTURE.md: %q", snippet)
		}
	}

	required := []string{
		"No `infra/*` → `control/*` imports remain.",
		"`internal/core` is removed and guarded by tests.",
	}
	for _, snippet := range required {
		if !strings.Contains(doc, snippet) {
			t.Fatalf("expected architecture claim missing in docs/arch/ARCHITECTURE.md: %q", snippet)
		}
	}
}
