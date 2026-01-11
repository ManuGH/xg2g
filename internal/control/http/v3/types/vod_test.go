package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVODPlaybackResponse_Contract verifies the JSON shape.
func TestVODPlaybackResponse_Contract(t *testing.T) {
	resp := VODPlaybackResponse{
		URL:             "/api/v3/stream.m3u8",
		Mode:            "hls",
		DurationSeconds: 120,
		Seekable:        true,
		Reason:          "resolved_via_test",
	}

	// 1. Verify JSON Marshal (Server Output)
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	expected := `{"url":"/api/v3/stream.m3u8","mode":"hls","duration_seconds":120,"seekable":true,"reason":"resolved_via_test"}`
	assert.JSONEq(t, expected, string(data), "JSON shape mismatch")

	// 2. Verify JSON Unmarshal (Client Input - strictness simulation)
	// Clients might use DisallowUnknownFields to detect API drift
	var loaded VODPlaybackResponse
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)
	assert.Equal(t, resp, loaded)
}
