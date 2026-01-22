package v3

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaybackInfo_SchemaCompliance(t *testing.T) {
	// 1. Generate a mock response
	reqID := "req_schema_test"
	info := PlaybackInfo{
		RequestId: reqID,
		SessionId: "sess_123",
		Mode:      PlaybackInfoModeHls,
		Url:       strPtr("/test.m3u8"),
	}

	w := httptest.NewRecorder()
	err := json.NewEncoder(w).Encode(info)
	require.NoError(t, err)

	// 2. Decode into a generic map to check for unexpected fields
	var raw map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	// 3. Define the allowlist of fields based on PlaybackInfo schema
	allowedFields := map[string]bool{
		"requestId":        true,
		"sessionId":        true,
		"mode":             true,
		"url":              true,
		"seekable":         true,
		"isSeekable":       true,
		"dvrWindowSeconds": true,
		"liveEdgeUnix":     true,
		"startUnix":        true,
		"durationSeconds":  true,
		"durationSource":   true,
		"resume":           true,
		"container":        true,
		"videoCodec":       true,
		"audioCodec":       true,
		"reason":           true,
		"decision":         true,
	}

	// 4. Assert no additional properties exist
	for field := range raw {
		assert.True(t, allowedFields[field], "Unexpected field found in response: %s (violates additionalProperties: false)", field)
	}
}
