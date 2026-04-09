package model

// RecordingStatus represents the authoritative coarse-grained state of a recording.
// JSON values are lower-case to match API conventions.
//
// RecordingStatusUnknown is an explicit truth-gap state: the system currently lacks
// confirmed recording truth and clients must not infer sub-causes from it. If finer
// causes become product-relevant, they must travel in a separate reason field rather
// than by overloading this status enum.
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
