// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
)

// TestOpenAPIPresence verifies that critical schemas and paths are present in the generated swagger spec.
// This ensures that we don't accidentally regress the API contract or fail to embed the spec.
func TestOpenAPIPresence(t *testing.T) {
	// 1. Load the embedded spec
	spec, err := GetSwagger()
	if err != nil {
		t.Fatalf("Failed to load swagger spec: %v", err)
	}

	// 2. Verify "Service" schema exists (was missing in previous regression)
	if _, ok := spec.Components.Schemas["Service"]; !ok {
		t.Error("Schema 'Service' is missing from OpenAPI components")
	}

	// 3. Verify Base Path assumptions
	// The openapi.yaml usually defines paths relative to root or usage of servers.
	// We check if "/sessions" exists.
	if spec.Paths.Find("/sessions") == nil {
		// Maybe it is defined as /api/v3/sessions in the spec?
		// check for that too
		if spec.Paths.Find("/api/v3/sessions") == nil {
			t.Error("Path '/sessions' (or /api/v3/sessions) is missing from OpenAPI paths")
		}
	}

	// 4. Verify Info
	if !strings.HasPrefix(spec.Info.Title, "xg2g") {
		t.Errorf("Unexpected API Title: %s", spec.Info.Title)
	}
}

// TestFactoryWiring ensures NewHandler enforces the expected middleware stack.
// Since we can't easily introspect the chi router middleware, we do a basic structural check
// or rely on integration tests (auth_strict_test) for behavior.
// Here we just ensure it doesn't panic on nil config.
func TestFactoryWiring(t *testing.T) {
	// We need a dummy server
	srv := &Server{}
	// And a config
	// Passing empty config should default to sane values or error if LANGuard fails (it handles empty list fine)

	// However, NewHandler calls middleware.NewLANGuard which splits TrustedProxies.
	// We expect it to succeed.

	_, err := NewHandler(srv, config.AppConfig{})
	if err != nil {
		t.Errorf("NewHandler failed with empty config: %v", err)
	}
}
