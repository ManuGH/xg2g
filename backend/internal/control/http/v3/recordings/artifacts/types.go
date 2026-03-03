package artifacts

import "time"

const (
	ArtifactKindPlaylist ArtifactKind = iota
	ArtifactKindSegmentTS
	ArtifactKindSegmentFMP4
	ArtifactKindSegmentInit
)

type ArtifactKind int

// ArtifactOK represents a successful artifact resolution
type ArtifactOK struct {
	// AbsPath is the absolute filesystem path (if servicing from disk directly)
	AbsPath string

	// Data is the in-memory content (if rewritten/generated)
	// If Data is non-nil, it takes precedence over AbsPath
	Data []byte

	ModTime time.Time
	Kind    ArtifactKind
}

// ArtifactError types (mapped to HTTP status by handler)
type ArtifactError struct {
	Code       ErrorCode
	Err        error
	Detail     string
	RetryAfter time.Duration
}

func (e *ArtifactError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Detail
}

type ErrorCode int

const (
	CodeInvalid   ErrorCode = iota // 400
	CodeNotFound                   // 404
	CodePreparing                  // 503
	CodeInternal                   // 500
)
