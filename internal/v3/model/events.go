// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

// EventType identifies a bus message type.
type EventType string

const (
	EventStartSession EventType = "session.start"
	EventStopSession  EventType = "session.stop"
	EventLeaseLost    EventType = "lease.lost"
	EventPipelineTick EventType = "pipeline.tick" // heartbeat/renew
)

// StartSessionEvent is emitted by the control-plane upon session intent.
type StartSessionEvent struct {
	Type          EventType `json:"type"`
	SessionID     string    `json:"sessionId"`
	ServiceRef    string    `json:"serviceRef"`
	ProfileID     string    `json:"profileId"`
	CorrelationID string    `json:"correlationId,omitempty"`
	RequestedAtUN int64     `json:"requestedAtUnix"`
}

// StopSessionEvent is emitted when a stop intent is received.
type StopSessionEvent struct {
	Type          EventType  `json:"type"`
	SessionID     string     `json:"sessionId"`
	Reason        ReasonCode `json:"reason,omitempty"`
	CorrelationID string     `json:"correlationId,omitempty"`
	RequestedAtUN int64      `json:"requestedAtUnix"`
}
