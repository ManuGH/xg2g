// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"io/fs"
	"os"
	"testing"
)

// TestUIEmbedContract verifies the WebUI embed contract:
// - dist/index.html exists (entry file) - HARD REQUIREMENT
// - At least one additional asset file exists - SOFT CHECK
//
// This test fails if WebUI assets are copied to the wrong location
// or if the embed pattern in ui.go is misconfigured.
func TestUIEmbedContract(t *testing.T) {
	// Verify dist/ can be opened as a subdirectory
	subFS, err := fs.Sub(uiFS, "dist")
	if err != nil {
		t.Fatalf("Failed to access 'dist' subdirectory in embedded FS: %v\n"+
			"This usually means WebUI assets were not copied to internal/control/http/dist/", err)
	}

	// HARD REQUIREMENT: index.html must exist (entry file)
	indexFile, err := subFS.Open("index.html")
	if err != nil {
		// Locally we allow API-only builds/tests without the WebUI bundle.
		// In CI/release builds we still want to enforce the embed contract.
		if os.Getenv("CI") == "" && os.Getenv("XG2G_UI_EMBED_REQUIRED") == "" {
			t.Skipf("dist/index.html not found in embedded FS (%v). Run 'make ui-build' to enable WebUI embed checks.", err)
		}
		t.Fatalf("dist/index.html not found in embedded FS: %v\n"+
			"Ensure 'make ui-build' or CI copies WebUI to internal/control/http/dist/", err)
	}
	indexFile.Close()

	// SOFT CHECK: Verify at least one additional file exists (assets, scripts, etc.)
	// This catches cases where only index.html was copied but build output was incomplete
	dirEntries, err := fs.ReadDir(subFS, ".")
	if err != nil {
		t.Fatalf("Failed to read dist/ directory: %v", err)
	}
	if len(dirEntries) < 2 {
		t.Logf("⚠️  Warning: Only %d file(s) in dist/ - expected index.html + assets", len(dirEntries))
	}

	// Optional: Check if assets/ exists (Vite default, but not required)
	if assetsDir, err := subFS.Open("assets"); err == nil {
		stat, _ := assetsDir.Stat()
		if stat != nil && stat.IsDir() {
			t.Logf("✅ assets/ directory found (Vite default structure)")
		}
		assetsDir.Close()
	}

	t.Logf("✅ WebUI embed contract verified: index.html present, %d total files", len(dirEntries))
}
