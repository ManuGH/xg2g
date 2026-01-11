package playback

// PlaybackMode defines the calculated strategy.
type PlaybackMode string

const (
	ModeDirectPlay   PlaybackMode = "direct_play"   // Client plays strict format directly
	ModeDirectStream PlaybackMode = "direct_stream" // Remux container only (no re-encode)
	ModeTranscode    PlaybackMode = "transcode"     // Re-encode required
	ModeError        PlaybackMode = "error"         // Hard failure (e.g. not found)
)

// ArtifactKind defines what kind of file/stream we point to.
type ArtifactKind string

const (
	ArtifactMP4  ArtifactKind = "mp4" // Single static file (e.g. stream.mp4)
	ArtifactHLS  ArtifactKind = "hls" // M3U8 Playlist (e.g. playlist.m3u8)
	ArtifactNone ArtifactKind = ""    // For errors
)

// ReasonCode defines strictly why a decision was made.
// Format: {PLATFORM}_{Constraint}_{Result}
type ReasonCode string

const (
	ReasonSafariDirectMP4   ReasonCode = "SAFARI_MP4_READY"
	ReasonSafariTSNeedsHLS  ReasonCode = "SAFARI_TS_REQUIRES_HLS"
	ReasonChromeDirectMP4   ReasonCode = "CHROME_MP4_READY"
	ReasonTranscodeRequired ReasonCode = "CODEC_MISMATCH_TRANSCODE"
	ReasonForceHLS          ReasonCode = "POLICY_FORCE_HLS"
	ReasonUnknownContainer  ReasonCode = "UNKNOWN_CONTAINER_TRANSCODE"
	ReasonFileNotFound      ReasonCode = "MEDIA_NOT_FOUND"
	ReasonProbeFailed       ReasonCode = "MEDIA_PROBE_FAILED"
	ReasonDirectPlayMatch   ReasonCode = "GENERIC_DIRECT_PLAY_READY"
	ReasonDirectStreamMatch ReasonCode = "GENERIC_DIRECT_STREAM_READY"
)

// MediaInfo represents pure facts about the recording.
// It is the output of the VODResolver.
type MediaInfo struct {
	// AbsPath is the absolute filesystem path.
	AbsPath string
	// Container is the detected format (e.g., "mp4", "mpegts", "mkv").
	Container string
	// VideoCodec e.g., "h264", "hevc", "mpeg2video".
	VideoCodec string
	// AudioCodec e.g., "aac", "mp2", "ac3".
	AudioCodec string
	// Duration in seconds.
	Duration float64
	// IsMP4FastPathEligible indicates if the file can be streamed directly (e.g. moov atom optimized).
	IsMP4FastPathEligible bool
}

// ClientProfile defines what the client can handle.
type ClientProfile struct {
	UserAgent string
	IsSafari  bool
	IsChrome  bool
	// Capabilities
	CanPlayTS   bool
	CanPlayHEVC bool
	CanPlayAC3  bool
}

// Policy allows overriding default behavior (e.g. force transcode).
type Policy struct {
	ForceTranscode bool
	ForceHLS       bool
}

// Decision represents the output of the engine.
type Decision struct {
	Mode     PlaybackMode
	Artifact ArtifactKind
	Reason   ReasonCode
}
