// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import "github.com/ManuGH/xg2g/internal/v3/model"

type IntentRequest struct {
	ServiceRef     string            `json:"serviceRef"`
	ProfileID      string            `json:"profile"`
	Params         map[string]string `json:"params,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
}

type IntentResponse struct {
	SessionID string           `json:"sessionId"`
	State     string           `json:"state"` // string for looser coupling in JSON? Or model.SessionState
	Reason    model.ReasonCode `json:"reason,omitempty"`
}

type SessionResponse struct {
	SessionID       string             `json:"sessionId"`
	ServiceRef      string             `json:"serviceRef"`
	ProfileID       string             `json:"profile"`
	State           model.SessionState `json:"state"`
	Reason          model.ReasonCode   `json:"reason,omitempty"`
	ReasonDetail    string             `json:"reasonDetail,omitempty"`
	CorrelationID   string             `json:"correlationId,omitempty"`
	UpdatedAtUnixMs int64              `json:"updatedAtMs"`
}
