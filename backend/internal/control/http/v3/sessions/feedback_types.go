// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package sessions

type PlaybackFeedbackEvent string

const (
	PlaybackFeedbackEventError   PlaybackFeedbackEvent = "error"
	PlaybackFeedbackEventWarning PlaybackFeedbackEvent = "warning"
	PlaybackFeedbackEventInfo    PlaybackFeedbackEvent = "info"
	PlaybackFeedbackEventStarted PlaybackFeedbackEvent = "started"
)

type PlaybackEngineErrorContextInput struct {
	AttemptId       *int    `json:"attemptId,omitempty"`
	Engine          *string `json:"engine,omitempty"`
	Phase           *string `json:"phase,omitempty"`
	PlaybackEpoch   *int    `json:"playbackEpoch,omitempty"`
	RecoveryAttempt *int    `json:"recoveryAttempt,omitempty"`
}

type PlaybackFeedbackInput struct {
	Code    *int                             `json:"code,omitempty"`
	Context *PlaybackEngineErrorContextInput `json:"context,omitempty"`
	Event   PlaybackFeedbackEvent            `json:"event"`
	Message *string                          `json:"message,omitempty"`
}

type PlaybackFeedbackResult struct {
	DecodeError   bool
	Accepted      bool
	NotFound      bool
	Unavailable   bool
	InternalError bool
	ErrorMessage  string
}
