package v3

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRFC7807_HeaderAssert enforces the Phase-7 Reality Gate contract.
// It verifies that V3 responses (specifically errors) strictly adhere to RFC 7807.
func TestRFC7807_HeaderAssert(t *testing.T) {
	// Setup server
	cfg := config.AppConfig{}
	server := NewServer(cfg, nil, nil)

	// Use a stubbed dependencies if needed, or rely on nil-checks handled by getters
	// The key is to trigger a known error path.
	// /api/v3/timers requires a source; if nil, it returns 503 via writeProblem.

	req := httptest.NewRequest("GET", "/api/v3/timers", nil)
	w := httptest.NewRecorder()

	server.GetTimers(w, req, GetTimersParams{})

	resp := w.Result()

	// 1. Assert Content-Type (Strict)
	ct := resp.Header.Get("Content-Type")
	assert.True(t, strings.HasPrefix(ct, "application/problem+json"), "V3 errors must use RFC 7807 Content-Type")

	// 2. Assert Body Shape
	var body map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &body)
	require.NoError(t, err, "V3 errors must be valid JSON")

	// 3. Assert Required Fields (RFC 7807 + V3 Extras)
	assert.NotEmpty(t, body["type"], "Problem must have 'type'")
	assert.NotEmpty(t, body["title"], "Problem must have 'title'")
	assert.Equal(t, 503.0, body["status"], "Problem status must match HTTP status")
	assert.Equal(t, "/api/v3/timers", body["instance"], "Problem must include 'instance' URI")
	assert.Equal(t, "UNAVAILABLE", body["code"], "Problem must include stable 'code' (V3 contract)")
}
