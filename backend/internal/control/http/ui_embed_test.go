// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package http

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// TestUIEmbedContract verifies the WebUI embed contract:
// - dist/index.html exists and is non-placeholder content
// - dist/assets contains real built assets (fail-closed)
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
	indexHTMLRaw, err := fs.ReadFile(subFS, "index.html")
	if err != nil {
		// Locally we allow API-only builds/tests without the WebUI bundle.
		// In CI/release builds we still want to enforce the embed contract.
		if os.Getenv("CI") == "" && os.Getenv("XG2G_UI_EMBED_REQUIRED") == "" {
			t.Skipf("dist/index.html not found in embedded FS (%v). Run 'make ui-build' to enable WebUI embed checks.", err)
		}
		t.Fatalf("dist/index.html not found in embedded FS: %v\n"+
			"Ensure 'make ui-build' or CI copies WebUI to internal/control/http/dist/", err)
	}
	indexHTML := strings.TrimSpace(string(indexHTMLRaw))
	if indexHTML == "" {
		t.Fatalf("dist/index.html is empty (placeholder artifact), expected real WebUI build output")
	}
	if strings.Contains(strings.ToLower(indexHTML), "ui not built") {
		t.Fatalf("dist/index.html contains placeholder fallback content, expected real bundled WebUI output")
	}

	// HARD REQUIREMENT: dist must contain more than index.html.
	dirEntries, err := fs.ReadDir(subFS, ".")
	if err != nil {
		t.Fatalf("Failed to read dist/ directory: %v", err)
	}
	if len(dirEntries) < 2 {
		t.Fatalf("dist/ has only %d file(s), expected index.html plus assets", len(dirEntries))
	}

	// HARD REQUIREMENT: Vite bundle must include assets/.
	assetsEntries, err := fs.ReadDir(subFS, "assets")
	if err != nil {
		t.Fatalf("dist/assets missing in embedded WebUI bundle: %v", err)
	}
	if len(assetsEntries) == 0 {
		t.Fatalf("dist/assets is empty, expected bundled JS/CSS assets")
	}

	hasJSAsset := false
	realAssetFiles := 0
	for _, entry := range assetsEntries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == ".gitkeep" || strings.Contains(name, "placeholder") {
			continue
		}
		realAssetFiles++
		if strings.HasSuffix(name, ".js") {
			hasJSAsset = true
		}
	}
	if realAssetFiles == 0 {
		t.Fatalf("dist/assets contains no real files (only placeholder artifacts)")
	}
	if !hasJSAsset {
		t.Fatalf("dist/assets contains no JS bundle file, expected at least one .js asset")
	}

	t.Logf("✅ WebUI embed contract verified: index.html + %d real asset file(s)", realAssetFiles)
}
