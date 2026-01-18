package model

// RecordingStatus represents the authoritative state of a recording.
// JSON values are lower-case to match API conventions.
type RecordingStatus string

const (
	RecordingStatusScheduled RecordingStatus = "scheduled"
	RecordingStatusRecording RecordingStatus = "recording"
	RecordingStatusCompleted RecordingStatus = "completed"
	RecordingStatusFailed    RecordingStatus = "failed"
	RecordingStatusUnknown   RecordingStatus = "unknown"
)

// FilePresenceClass represents the truthful categorization of a recording file's presence.
// It abstracts away raw file checks (size, extensions) into domain-significant states.
type FilePresenceClass string

const (
	FilePresenceMissing FilePresenceClass = "missing"
	FilePresenceSmall   FilePresenceClass = "small"   // File exists but is below viability threshold (< 10MB)
	FilePresencePartial FilePresenceClass = "partial" // File exists but has transient suffix (.part, .tmp)
	FilePresenceOK      FilePresenceClass = "ok"      // File exists and is viable
)
