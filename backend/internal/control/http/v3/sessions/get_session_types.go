package sessions

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

// GetSessionRequest is the transport-neutral request payload for looking up one session.
type GetSessionRequest struct {
	SessionID string
	RequestID string
	Now       time.Time
	HLSRoot   string
}

// GetSessionResult holds the prepared domain data needed by the HTTP adapter.
type GetSessionResult struct {
	Session      *model.SessionRecord
	Outcome      lifecycle.PublicOutcome
	PlaybackInfo SessionPlaybackInfo
}

// SessionPlaybackInfo holds transport-neutral playback window data derived from a session record.
type SessionPlaybackInfo struct {
	Mode                 string
	DurationSeconds      *float64
	SeekableStartSeconds *float64
	SeekableEndSeconds   *float64
	LiveEdgeSeconds      *float64
}

// GetSessionTerminal captures canonical terminal-session information for the HTTP adapter.
type GetSessionTerminal struct {
	Session      *model.SessionRecord
	Outcome      lifecycle.PublicOutcome
	State        model.SessionState
	Reason       model.ReasonCode
	ReasonDetail string
	Code         string
	Detail       string
}

type GetSessionErrorKind uint8

const (
	GetSessionErrorUnavailable GetSessionErrorKind = iota
	GetSessionErrorInvalidInput
	GetSessionErrorNotFound
	GetSessionErrorTerminal
	GetSessionErrorInternal
)

// GetSessionError captures transport-neutral failures from session lookup.
type GetSessionError struct {
	Kind     GetSessionErrorKind
	Message  string
	Terminal *GetSessionTerminal
	Cause    error
}

func (e *GetSessionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Terminal != nil && e.Terminal.Detail != "" {
		return e.Terminal.Detail
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "get session error"
}
