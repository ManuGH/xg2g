// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import "github.com/ManuGH/xg2g/internal/domain/session/model"

type IntentRequest struct {
	Type       model.IntentType `json:"type"` // defaults to stream.start if empty
	SessionID  string           `json:"sessionId,omitempty"`
	ServiceRef string           `json:"serviceRef"`

	CorrelationID  string            `json:"correlationId,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	StartMs        *int64            `json:"startMs,omitempty"` // Seeking offset in ms
}

type IntentResponse struct {
	SessionID     string `json:"sessionId"`
	Status        string `json:"status"`
	CorrelationID string `json:"correlationId,omitempty"`
}

type SessionResponse struct {
	SessionID            string             `json:"sessionId"`
	ServiceRef           string             `json:"serviceRef"`
	Profile              string             `json:"profile"`
	State                model.SessionState `json:"state"`
	Reason               model.ReasonCode   `json:"reason,omitempty"`
	ReasonDetail         string             `json:"reasonDetail,omitempty"`
	CorrelationID        string             `json:"correlationId,omitempty"`
	UpdatedAtMs          int64              `json:"updatedAtMs"`
	Mode                 string             `json:"mode,omitempty"`
	DurationSeconds      *float64           `json:"durationSeconds,omitempty"`
	SeekableStartSeconds *float64           `json:"seekableStartSeconds,omitempty"`
	SeekableEndSeconds   *float64           `json:"seekableEndSeconds,omitempty"`
	LiveEdgeSeconds      *float64           `json:"liveEdgeSeconds,omitempty"`
	PlaybackURL          string             `json:"playbackUrl,omitempty"`
}
