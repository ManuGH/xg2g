// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package log

// Canonical field name constants for structured logging.
const (
	// Identity fields
	FieldSessionID       = "session_id"
	FieldCorrelationID   = "correlation_id"
	FieldRequestID       = "request_id"
	FieldClientRequestID = "client_request_id"
	FieldJobID           = "job_id"
	FieldTimerID         = "timer_id"
	FieldMetaID          = "meta_id"
	FieldServiceRef      = "service_ref"

	// Process / pipeline fields
	FieldEvent     = "event"
	FieldComponent = "component"
	FieldHandle    = "handle"

	// Media / stream fields
	FieldCodec      = "codec"
	FieldResolution = "resolution"
	FieldFPS        = "fps"
	FieldEncoder    = "encoder"
	FieldDevice     = "device"

	// State fields
	FieldOldState = "old_state"
	FieldNewState = "new_state"

	// Path / URL fields
	FieldPath         = "path"
	FieldBaseURL      = "base_url"
	FieldFinalPath    = "final_path"
	FieldPlaylistPath = "playlist_path"

	// Network fields
	FieldStreamPort = "stream_port"
)
