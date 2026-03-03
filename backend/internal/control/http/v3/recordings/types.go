package recordings

import "github.com/ManuGH/xg2g/internal/control/clientplayback"

// ClientPlaybackRequest is the transport-agnostic request payload consumed by the recordings service.
type ClientPlaybackRequest = clientplayback.PlaybackInfoRequest

// ClientPlaybackResponse is the transport-agnostic response payload produced by the recordings service.
type ClientPlaybackResponse = clientplayback.PlaybackInfoResponse

type ClientPlaybackErrorKind uint8

const (
	ClientPlaybackErrorUnavailable ClientPlaybackErrorKind = iota
	ClientPlaybackErrorInvalidInput
	ClientPlaybackErrorNotFound
	ClientPlaybackErrorPreparing
	ClientPlaybackErrorUpstreamUnavailable
	ClientPlaybackErrorInternal
)

// ClientPlaybackError captures non-HTTP playback info failures.
type ClientPlaybackError struct {
	Kind              ClientPlaybackErrorKind
	Message           string
	RetryAfterSeconds int
	ProbeState        string
	Cause             error
}

func (e *ClientPlaybackError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "client playback error"
}
