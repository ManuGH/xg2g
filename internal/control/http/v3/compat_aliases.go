package v3

const (
	// Compatibility aliases for mode values introduced in newer schema revisions.
	PlaybackInfoModeNativeHls PlaybackInfoMode = "native_hls"
	PlaybackInfoModeHlsjs     PlaybackInfoMode = "hlsjs"
	PlaybackInfoModeTranscode PlaybackInfoMode = "transcode"

	// Compatibility aliases for conflict type values referenced by tests/helpers.
	Duplicate TimerConflictType = TimerConflictTypeDuplicate
	Overlap   TimerConflictType = TimerConflictTypeOverlap
)
