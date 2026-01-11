package types

// VODPlaybackResponse represents the contract for playing a Recording.
// This DTO must contain ALL information required by the UI to start playback.
// No derived state logic is permitted in the UI.
type VODPlaybackResponse struct {
	// StreamURL is the absolute or relative URL to the playback manifest/file.
	// The backend determines the best format (HLS/MP4) based on client capabilities and config.
	StreamURL string `json:"stream_url"`

	// PlaybackType indicates the mode: "hls", "mp4", etc.
	// UI behaves differently based on this (e.g., using HLS.js vs native video tag).
	PlaybackType string `json:"playback_type"`

	// DurationSeconds is the authoritative duration.
	// UI must not guess this from file headers.
	DurationSeconds int64 `json:"duration_seconds"`

	// MimeType provides the Content-Type for the player source.
	MimeType string `json:"mime_type"`

	// RecordingID echoes the requested ID for context.
	RecordingID string `json:"recording_id"`

	// DurationSource indicates where the duration truth came from: "cache", "store", "probe".
	DurationSource string `json:"duration_source,omitempty"`
}

// VODPlaybackError is a specialized error breakdown (optional, usually RFC7807 is sufficient).
// We adhere to pure RFC7807 for errors, so no specific error struct is needed here unless data-rich.

const (
	// PlaybackTypeHLS indicates HTTP Live Streaming (m3u8).
	PlaybackTypeHLS = "hls"
	// PlaybackTypeMP4 indicates direct progressive download (mp4).
	PlaybackTypeMP4 = "mp4"
)
