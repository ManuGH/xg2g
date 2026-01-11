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
		StreamURL:       "/api/v3/stream.m3u8",
		PlaybackType:    "hls",
		DurationSeconds: 120,
		MimeType:        "application/vnd.apple.mpegurl",
		RecordingID:     "rec-123",
	}

	// 1. Verify JSON Marshal (Server Output)
	data, err := json.Marshal(resp)
	require.NoError(t, err)

	expected := `{"stream_url":"/api/v3/stream.m3u8","playback_type":"hls","duration_seconds":120,"mime_type":"application/vnd.apple.mpegurl","recording_id":"rec-123"}`
	assert.JSONEq(t, expected, string(data), "JSON shape mismatch")

	// 2. Verify JSON Unmarshal (Client Input - strictness simulation)
	// Clients might use DisallowUnknownFields to detect API drift
	var loaded VODPlaybackResponse
	err = json.Unmarshal(data, &loaded)
	require.NoError(t, err)
	assert.Equal(t, resp, loaded)
}
