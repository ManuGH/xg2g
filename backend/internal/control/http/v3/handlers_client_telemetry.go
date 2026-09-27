// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0.

package v3

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/problemcode"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const maxClientTelemetryBodyBytes = 8 * 1024

// handleSessionClientTelemetry accepts one bounded client playback snapshot and
// fans it out to subscribers of the addressed live session.
func (s *Server) handleSessionClientTelemetry(w http.ResponseWriter, r *http.Request, sessionID openapi_types.UUID) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxClientTelemetryBodyBytes))
	if err != nil {
		metrics.RecordClientPlaybackTelemetry("unknown", "invalid", 0, 0, 0, 0, 0)
		if isTelemetryBodyTooLarge(err) {
			writeRegisteredProblem(w, r, http.StatusRequestEntityTooLarge, "sessions/telemetry/too_large", "Telemetry Payload Too Large", problemcode.CodeInvalidInput, "The telemetry payload exceeds the 8 KiB limit.", nil)
			return
		}
		writeRegisteredProblem(w, r, http.StatusBadRequest, "sessions/telemetry/invalid", "Invalid Telemetry", problemcode.CodeInvalidInput, "The telemetry payload is malformed.", nil)
		return
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(body, &present); err != nil || present == nil {
		metrics.RecordClientPlaybackTelemetry("unknown", "invalid", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusBadRequest, "sessions/telemetry/invalid", "Invalid Telemetry", problemcode.CodeInvalidInput, "The request must contain one JSON object.", nil)
		return
	}
	for _, required := range []string{
		"platform", "observedAt", "decodedFps", "audioLeadMs", "audioUnderruns",
		"ingestGapCount250Ms", "ingestGapCount600Ms", "ingestGapCount1000Ms",
		"ingestGapLastMs", "ingestGapMaxMs", "continuityErrors", "continuityErrorsDelta",
		"decodeErrors", "decodeErrorsDelta", "ptsDiscontinuities", "ingestBacklogBytes",
		"droppedFrames", "lateFrames", "thermalState",
	} {
		value, exists := present[required]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			metrics.RecordClientPlaybackTelemetry("unknown", "invalid", 0, 0, 0, 0, 0)
			writeRegisteredProblem(w, r, http.StatusBadRequest, "sessions/telemetry/invalid", "Invalid Telemetry", problemcode.CodeInvalidInput, "A required telemetry field is missing or null.", nil)
			return
		}
	}

	var req SessionClientTelemetryRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		metrics.RecordClientPlaybackTelemetry("unknown", "invalid", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusBadRequest, "sessions/telemetry/invalid", "Invalid Telemetry", problemcode.CodeInvalidInput, "The telemetry payload is malformed or contains unsupported fields.", nil)
		return
	}

	if err := validateClientTelemetry(req, time.Now()); err != nil {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "invalid", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusBadRequest, "sessions/telemetry/invalid", "Invalid Telemetry", problemcode.CodeInvalidInput, err.Error(), nil)
		return
	}

	deps := s.sessionsModuleDeps()
	if deps.store == nil {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "unavailable", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "sessions/telemetry/unavailable", "Session Telemetry Unavailable", problemcode.CodeServiceUnavailable, "The session store is unavailable.", nil)
		return
	}
	session, err := deps.store.GetSession(r.Context(), sessionID.String())
	if err != nil {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "unavailable", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "sessions/telemetry/unavailable", "Session Telemetry Unavailable", problemcode.CodeStoreError, "The session state could not be read.", nil)
		return
	}
	if session == nil {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "not_found", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusNotFound, "sessions/not_found", "Session Not Found", problemcode.CodeSessionDotNotFound, "The requested session does not exist.", nil)
		return
	}
	if session.State.IsTerminal() || (session.LeaseExpiresAtUnix > 0 && time.Now().Unix() > session.LeaseExpiresAtUnix) {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "closed", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusGone, "sessions/telemetry/closed", "Session Closed", problemcode.CodeSessionExpired, "Telemetry is accepted only while the session is active.", nil)
		return
	}
	if deps.bus == nil {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "unavailable", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "sessions/telemetry/unavailable", "Session Telemetry Unavailable", problemcode.CodeServiceUnavailable, "The session event bus is unavailable.", nil)
		return
	}

	telemetry := model.ClientPlaybackTelemetry{
		Platform:              string(req.Platform),
		ObservedAt:            req.ObservedAt,
		DecodedFPS:            float64(req.DecodedFps),
		AudioLeadMs:           float64(req.AudioLeadMs),
		AudioUnderruns:        req.AudioUnderruns,
		IngestGapCount250Ms:   req.IngestGapCount250Ms,
		IngestGapCount600Ms:   req.IngestGapCount600Ms,
		IngestGapCount1000Ms:  req.IngestGapCount1000Ms,
		IngestGapLastMs:       float64(req.IngestGapLastMs),
		IngestGapMaxMs:        float64(req.IngestGapMaxMs),
		ContinuityErrors:      req.ContinuityErrors,
		ContinuityErrorsDelta: req.ContinuityErrorsDelta,
		DecodeErrors:          req.DecodeErrors,
		DecodeErrorsDelta:     req.DecodeErrorsDelta,
		PTSDiscontinuities:    req.PtsDiscontinuities,
		IngestBacklogBytes:    req.IngestBacklogBytes,
		DroppedFrames:         req.DroppedFrames,
		LateFrames:            req.LateFrames,
		ThermalState:          string(req.ThermalState),
	}
	if req.AppVersion != nil {
		telemetry.AppVersion = strings.TrimSpace(*req.AppVersion)
	}
	event := model.SessionClientTelemetryEvent{
		Type:       model.EventSessionClientTelemetry,
		SessionID:  sessionID.String(),
		Telemetry:  telemetry,
		ReceivedAt: time.Now().UTC(),
	}
	if err := deps.bus.Publish(r.Context(), string(model.EventSessionClientTelemetry), event); err != nil {
		metrics.RecordClientPlaybackTelemetry(string(req.Platform), "unavailable", 0, 0, 0, 0, 0)
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "sessions/telemetry/publish_failed", "Session Telemetry Unavailable", problemcode.CodeServiceUnavailable, "The telemetry event could not be published.", nil)
		return
	}

	metrics.RecordClientPlaybackTelemetry(
		string(req.Platform),
		"accepted",
		float64(req.DecodedFps),
		float64(req.AudioLeadMs),
		float64(req.IngestGapMaxMs),
		req.ContinuityErrorsDelta,
		req.DecodeErrorsDelta,
	)
	w.WriteHeader(http.StatusAccepted)
}

func validateClientTelemetry(req SessionClientTelemetryRequest, now time.Time) error {
	switch req.Platform {
	case "ios", "tvos", "android", "web":
	default:
		return errors.New("platform must be ios, tvos, android, or web")
	}
	if req.ThermalState != "Nominal" && req.ThermalState != "Fair" && req.ThermalState != "Serious" && req.ThermalState != "Critical" && req.ThermalState != "Unknown" {
		return errors.New("thermalState is not a supported value")
	}
	if req.AppVersion != nil && len(*req.AppVersion) > 64 {
		return errors.New("appVersion must be at most 64 characters")
	}
	if req.ObservedAt.IsZero() || req.ObservedAt.Before(now.Add(-24*time.Hour)) || req.ObservedAt.After(now.Add(5*time.Minute)) {
		return errors.New("observedAt must be within the last 24 hours and no more than 5 minutes in the future")
	}
	if req.DecodedFps < 0 || req.DecodedFps > 240 || req.AudioLeadMs < 0 || req.AudioLeadMs > 120_000 || req.IngestGapLastMs < 0 || req.IngestGapLastMs > 120_000 || req.IngestGapMaxMs < req.IngestGapLastMs || req.IngestGapMaxMs > 120_000 {
		return errors.New("playback timing values are outside their supported range")
	}
	counts := []int{
		req.AudioUnderruns,
		req.IngestGapCount250Ms,
		req.IngestGapCount600Ms,
		req.IngestGapCount1000Ms,
		req.ContinuityErrors,
		req.ContinuityErrorsDelta,
		req.DecodeErrors,
		req.DecodeErrorsDelta,
		req.PtsDiscontinuities,
		req.DroppedFrames,
		req.LateFrames,
	}
	for _, count := range counts {
		if count < 0 || count > 100_000_000 {
			return errors.New("telemetry counters are outside their supported range")
		}
	}
	if req.IngestBacklogBytes < 0 || req.IngestBacklogBytes > 64*1024*1024 {
		return errors.New("ingestBacklogBytes is outside its supported range")
	}
	if req.IngestGapCount1000Ms > req.IngestGapCount600Ms || req.IngestGapCount600Ms > req.IngestGapCount250Ms {
		return errors.New("ingest gap counters must be cumulative by threshold")
	}
	return nil
}

func isTelemetryBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}
