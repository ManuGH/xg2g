package vod

// StreamInfo defines the properties needed for decision making.
type StreamInfo struct {
	Video VideoStreamInfo
	Audio AudioStreamInfo
}

type VideoStreamInfo struct {
	CodecName  string
	PixFmt     string
	Profile    string
	Level      int
	BitDepth   int
	StartTime  float64
	Duration   float64
	Width      int
	Height     int
	Interlaced bool
}

type AudioStreamInfo struct {
	CodecName     string
	SampleRate    int
	Channels      int
	ChannelLayout string
	TrackCount    int
	StartTime     float64
}

// JobStatus is a stable DTO for API consumption.
// It abstracts internal state.
type JobStatus struct {
	State     JobState // Enum: Idle, Building, Finalizing, Succeeded, Failed
	Reason    string
	UpdatedAt int64 // Unix timestamp
}

type JobState string

const (
	JobStateIdle       JobState = "IDLE"
	JobStateBuilding   JobState = "BUILDING"
	JobStateFinalizing JobState = "FINALIZING"
	JobStateSucceeded  JobState = "SUCCEEDED"
	JobStateFailed     JobState = "FAILED"
)
