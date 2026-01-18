package recordings

import (
	"time"

	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/domain/recordings/model"
)

type PlaybackIntent string

const (
	IntentMetadata PlaybackIntent = "metadata"
	IntentStream   PlaybackIntent = "stream"
)

const (
	ReasonDirectPlayMatch = "directplay_match"
	ReasonTranscodeAudio  = "transcode_audio"
	ReasonTranscodeVideo  = "transcode_video"
)

type PlaybackProfile string

const (
	ProfileSafari  PlaybackProfile = "safari"
	ProfileGeneric PlaybackProfile = "generic"
	ProfileTVOS    PlaybackProfile = "tvOS"
)

type PlaybackDecision string

const (
	DecisionDirectPlay   PlaybackDecision = "direct_play"
	DecisionDirectStream PlaybackDecision = "direct_stream"
	DecisionTranscode    PlaybackDecision = "transcoder"
)

type CachePolicy string

const (
	CacheNoStore  CachePolicy = "no-store"
	CacheShort    CachePolicy = "short"
	CacheSegments CachePolicy = "segments"
)

type ListInput struct {
	RootID      string
	Path        string
	PrincipalID string
}

type RecordingItem struct {
	ServiceRef       string
	RecordingID      string
	Title            string
	Description      string
	BeginUnixSeconds int64
	DurationSeconds  *int64
	Length           string
	Filename         string
	Status           model.RecordingStatus
	Resume           *ResumeSummary
}

type ResumeSummary struct {
	PosSeconds      int64
	DurationSeconds int64
	Finished        bool
	UpdatedAt       *time.Time
}

type DirectoryItem struct {
	Name string
	Path string
}

type Breadcrumb struct {
	Name string
	Path string
}

type RecordingRoot struct {
	ID   string
	Name string
}

type ListResult struct {
	Roots       []RecordingRoot
	CurrentRoot string
	CurrentPath string
	Recordings  []RecordingItem
	Directories []DirectoryItem
	Breadcrumbs []Breadcrumb
}

type PlaybackInfoInput struct {
	RecordingID string
	Intent      string
	Profile     PlaybackProfile
}

type DurationSource string

const (
	DurationSourceStore DurationSource = "store"
	DurationSourceCache DurationSource = "cache"
	DurationSourceProbe DurationSource = "probe"
)

type PlaybackInfoResult struct {
	Decision        playback.Decision
	MediaInfo       playback.MediaInfo
	Reason          string
	DurationSeconds *int64
	DurationSource  *DurationSource
	Container       *string
	VideoCodec      *string
	AudioCodec      *string
}

type ResolveCode string

const (
	CodePreparing ResolveCode = "PREPARING"
	CodeInvalid   ResolveCode = "INVALID_ID"
	CodeNotFound  ResolveCode = "VOD_NOT_FOUND"
	CodeUpstream  ResolveCode = "UPSTREAM_UNAVAILABLE"
	CodeFailed    ResolveCode = "VOD_PLAYBACK_ERROR"
	CodeInternal  ResolveCode = "INTERNAL_ERROR"
)

type StatusInput struct {
	RecordingID string
}

type StatusResult struct {
	State string
	Error *string
}

type StreamInput struct {
	RecordingID string
}

type StreamResult struct {
	Ready       bool
	LocalPath   string
	State       string // For 503 response (e.g. PREPARING, UNKNOWN)
	RetryAfter  int    // Retry-After header value in seconds
	CachePolicy CachePolicy
}

type DeleteInput struct {
	RecordingID string
}

type DeleteResult struct {
	Deleted bool
}
