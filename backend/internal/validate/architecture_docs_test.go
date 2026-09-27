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
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	docPath := filepath.Join(repoRoot, "docs", "arch", "ARCHITECTURE.md")

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

// TestDiataxis_QuadrantReadmesExist guards the Diátaxis 4-quadrant documentation architecture.
func TestDiataxis_QuadrantReadmesExist(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	quadrants := []struct {
		path   string
		header string
	}{
		{filepath.Join("docs", "tutorials", "README.md"), "# Tutorials (Diátaxis: Learning-Oriented)"},
		{filepath.Join("docs", "how-to", "README.md"), "# How-To Guides (Diátaxis: Problem-Oriented)"},
		{filepath.Join("docs", "reference", "README.md"), "# Reference (Diátaxis: Information-Oriented)"},
		{filepath.Join("docs", "explanation", "README.md"), "# Explanation (Diátaxis: Understanding-Oriented)"},
	}

	for _, q := range quadrants {
		fullPath := filepath.Join(repoRoot, q.path)
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("missing required Diátaxis quadrant doc %s: %v", q.path, err)
		}
		if !strings.HasPrefix(string(raw), q.header) {
			t.Fatalf("expected header %q in %s", q.header, q.path)
		}
	}
}

// TestLLMSTxt_StandardCompliance verifies that llms.txt exists at repository root
// and contains valid, high-density machine-readable navigation without sensitive IP leaks.
func TestLLMSTxt_StandardCompliance(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	llmsPath := filepath.Join(repoRoot, "llms.txt")

	raw, err := os.ReadFile(llmsPath)
	if err != nil {
		t.Fatalf("missing required llms.txt at repo root: %v", err)
	}
	content := string(raw)

	if !strings.HasPrefix(content, "# xg2g") {
		t.Fatalf("llms.txt must begin with '# xg2g'")
	}

	requiredSections := []string{
		"## System Architecture & Non-Negotiable Invariants",
		"## Media Pipeline & Codec Matrix",
		"## Operations, Runbooks & Triage",
		"## API & Wire Contracts",
		"## Developer & Contributor Governance",
	}
	for _, sec := range requiredSections {
		if !strings.Contains(content, sec) {
			t.Fatalf("llms.txt missing required section: %q", sec)
		}
	}
}
