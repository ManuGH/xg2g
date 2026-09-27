// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package model

import (
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
)

// EventType identifies a bus message type.
type EventType string

const (
	EventStartSession        EventType = "session.start"
	EventStopSession         EventType = "session.stop"
	EventLeaseLost           EventType = "lease.lost"
	EventPipelineTick        EventType = "pipeline.tick" // heartbeat/renew
	EventSessionStateChanged EventType = "session.state_changed"
	EventSessionTelemetry    EventType = "session.telemetry"
	EventSessionClientTelemetry EventType = "session.client_telemetry"
)

// StartSessionEvent is emitted by the control-plane upon session intent.
type StartSessionEvent struct {
	Type          EventType `json:"type"`
	SessionID     string    `json:"sessionId"`
	ServiceRef    string    `json:"serviceRef"`
	ProfileID     string    `json:"profileId"`
	CorrelationID string    `json:"correlationId,omitempty"`
	RequestedAtUN int64     `json:"requestedAtUnix"`
	StartMs       int64     `json:"startMs,omitempty"`
}

// StopSessionEvent is emitted when a stop intent is received.
type StopSessionEvent struct {
	Type          EventType  `json:"type"`
	SessionID     string     `json:"sessionId"`
	Reason        ReasonCode `json:"reason,omitempty"`
	CorrelationID string     `json:"correlationId,omitempty"`
	RequestedAtUN int64      `json:"requestedAtUnix"`
}

// SessionStateChangedEvent is emitted whenever a session transitions lifecycle state.
type SessionStateChangedEvent struct {
	Type        EventType    `json:"type"`
	SessionID   string       `json:"sessionId"`
	State       SessionState `json:"state"`
	Reason      ReasonCode   `json:"reason,omitempty"`
	UpdatedAtUN int64        `json:"updatedAtUnix"`
}

// SessionTelemetryEvent is emitted whenever real-time encoding diagnostics are updated.
type SessionTelemetryEvent struct {
	Type        EventType                `json:"type"`
	SessionID   string                   `json:"sessionId"`
	Diagnostics ports.RuntimeDiagnostics `json:"diagnostics"`
	UpdatedAtUN int64                    `json:"updatedAtUnix"`
}

// ClientPlaybackTelemetry contains a bounded client-side snapshot without device,
// channel, URL, or free-form log data.
type ClientPlaybackTelemetry struct {
	Platform              string    `json:"platform"`
	AppVersion            string    `json:"appVersion,omitempty"`
	ObservedAt            time.Time `json:"observedAt"`
	DecodedFPS            float64   `json:"decodedFps"`
	AudioLeadMs           float64   `json:"audioLeadMs"`
	AudioUnderruns        int       `json:"audioUnderruns"`
	IngestGapCount250Ms   int       `json:"ingestGapCount250Ms"`
	IngestGapCount600Ms   int       `json:"ingestGapCount600Ms"`
	IngestGapCount1000Ms  int       `json:"ingestGapCount1000Ms"`
	IngestGapLastMs       float64   `json:"ingestGapLastMs"`
	IngestGapMaxMs        float64   `json:"ingestGapMaxMs"`
	ContinuityErrors      int       `json:"continuityErrors"`
	ContinuityErrorsDelta int       `json:"continuityErrorsDelta"`
	DecodeErrors          int       `json:"decodeErrors"`
	DecodeErrorsDelta     int       `json:"decodeErrorsDelta"`
	PTSDiscontinuities    int       `json:"ptsDiscontinuities"`
	IngestBacklogBytes    int       `json:"ingestBacklogBytes"`
	DroppedFrames         int       `json:"droppedFrames"`
	LateFrames            int       `json:"lateFrames"`
	ThermalState          string    `json:"thermalState"`
}

// SessionClientTelemetryEvent fans a client snapshot out to live session observers.
type SessionClientTelemetryEvent struct {
	Type        EventType                `json:"type"`
	SessionID   string                   `json:"sessionId"`
	Telemetry   ClientPlaybackTelemetry   `json:"telemetry"`
	ReceivedAt  time.Time                 `json:"receivedAt"`
}
