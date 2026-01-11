package types

// VODPlaybackResponse represents the contract for playing a Recording.
// This DTO must contain ALL information required by the UI to start playback.
// No derived state logic is permitted in the UI.
// Fields match OpenAPI spec (PlaybackInfo schema).
type VODPlaybackResponse struct {
	// Mode indicates the playback type: "hls", "direct_mp4", etc.
	// UI behaves differently based on this (e.g., using HLS.js vs native video tag).
	Mode string `json:"mode"`

	// URL is the absolute or relative URL to the playback manifest/file.
	// The backend determines the best format (HLS/MP4) based on client capabilities and config.
	URL string `json:"url"`

	// DurationSeconds is the authoritative duration.
	// UI must not guess this from file headers.
	DurationSeconds int64 `json:"duration_seconds,omitempty"`

	// Seekable indicates if the media supports seeking.
	Seekable bool `json:"seekable,omitempty"`

	// Reason indicates resolution path: "resolved_via_store", "probed_and_persisted", etc.
	Reason string `json:"reason,omitempty"`
}

// VODPlaybackError is a specialized error breakdown (optional, usually RFC7807 is sufficient).
// We adhere to pure RFC7807 for errors, so no specific error struct is needed here unless data-rich.

const (
	// PlaybackTypeHLS indicates HTTP Live Streaming (m3u8).
	PlaybackTypeHLS = "hls"
	// PlaybackTypeMP4 indicates direct progressive download (mp4).
	PlaybackTypeMP4 = "mp4"
)
