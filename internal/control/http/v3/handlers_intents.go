// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
)

// Responsibility: Handles Intent creation (Start/Stop stream signals).
// Non-goals: Providing stream data or session status.

const (
	admissionLeaseTTL      = 30 * time.Second
	admissionRetryAfterSec = 1
)

// handleV3Intents handles POST /api/v3/intents.
func (s *Server) handleV3Intents(w http.ResponseWriter, r *http.Request) {
	// 0. Hardening: Limit Request Size (1MB)
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	// Get Config Snapshot for consistent view during request
	cfg := s.GetConfig()

	// 1. Verify V3 Components Available
	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if bus == nil || store == nil {
		// V3 Worker not running
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 control plane not enabled",
		})
		return
	}

	// 2. Decode Request
	var req v3api.IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // Hardening
	if err := dec.Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}
	correlationID, err := v3api.NormalizeCorrelationID(req.CorrelationID)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
		return
	}

	logger := log.WithComponentFromContext(r.Context(), "api")
	correlationProvided := correlationID != ""
	if correlationProvided {
		logger = logger.With().Str("correlationId", correlationID).Logger()
	}

	intentType := req.Type
	if intentType == "" {
		intentType = model.IntentTypeStreamStart
	}

	// 3. Compute Idempotency Key (Server-Side)
	var idempotencyKey string
	if intentType == model.IntentTypeStreamStart {
		// ADR-00X: Universal Delivery Policy
		// Profile selection is removed. We enforce "universal".
		requestedProfile := "universal"

		// Bucket: 0 for Live, StartTime/1000 for VOD
		bucket := "0"
		if req.StartMs != nil && *req.StartMs > 0 {
			// VOD/Catchup bucket (1 second resolution)
			bucket = fmt.Sprintf("%d", *req.StartMs/1000)
		}

		// Compute Usage Key
		idempotencyKey = ComputeIdemKey(model.IntentTypeStreamStart, req.ServiceRef, requestedProfile, bucket)
	}

	// 4. Generate Session ID (Strong UUID)
	var sessionID string
	switch intentType {
	case model.IntentTypeStreamStart:
		// New Session
		sessionID = uuid.New().String()
		if correlationID == "" {
			correlationID = uuid.New().String()
		}
	case model.IntentTypeStreamStop:
		// STOP logic remains same
		if req.SessionID == "" {
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "sessionId required for stop")
			return
		}
		sessionID = req.SessionID
		if correlationID == "" {
			if session, err := store.GetSession(r.Context(), sessionID); err == nil && session != nil {
				correlationID = session.CorrelationID
			}
		}
	default:
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "unsupported intent type")
		return
	}

	if !correlationProvided && correlationID != "" {
		logger = logger.With().Str("correlationId", correlationID).Logger()
	}

	mode := model.ModeLive
	if raw := strings.TrimSpace(req.Params["mode"]); raw != "" {
		if strings.EqualFold(raw, model.ModeRecording) {
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "recording playback uses /recordings")
			return
		}
		if !strings.EqualFold(raw, model.ModeLive) {
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "unsupported playback mode")
			return
		}
	}

	// 5. Build & Publish Event
	switch intentType {
	case model.IntentTypeStreamStart:
		// Re-resolve profileSpec to get details (Name, etc)
		reqProfileID := "universal"

		// Smart Profile Lookup
		var cap *scan.Capability
		if s.v3Scan != nil {
			if c, found := s.v3Scan.GetCapability(req.ServiceRef); found {
				cap = &c
			}
		}
		hasGPU := hardware.HasVAAPI()

		// Parse hwaccel parameter (v3.1+)
		hwaccelMode := profiles.HWAccelAuto // Default
		if hwaccel := strings.TrimSpace(req.Params["hwaccel"]); hwaccel != "" {
			switch strings.ToLower(hwaccel) {
			case "force":
				hwaccelMode = profiles.HWAccelForce
			case "off":
				hwaccelMode = profiles.HWAccelOff
			case "auto":
				hwaccelMode = profiles.HWAccelAuto
			default:
				// Strict validation: unknown hwaccel → 400 Bad Request
				RecordV3Intent(string(model.IntentTypeStreamStart), "phase0", "invalid_hwaccel")
				RespondError(w, r, http.StatusBadRequest, ErrInvalidInput,
					fmt.Sprintf("invalid hwaccel value: %q (must be auto, force, or off)", hwaccel))
				return
			}
		}

		// Hard-fail if force requested but no GPU available
		if hwaccelMode == profiles.HWAccelForce && !hasGPU {
			RecordV3Intent(string(model.IntentTypeStreamStart), "phase0", "hwaccel_unavailable")
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput,
				"hwaccel=force requested but GPU not available (no /dev/dri/renderD128)")
			return
		}

		profileSpec := profiles.Resolve(reqProfileID, r.UserAgent(), int(cfg.HLS.DVRWindow.Seconds()), cap, hasGPU, hwaccelMode)

		// 5.1 Admission Control Gate (Phase 5.2)
		priority := admission.PriorityLive
		if strings.Contains(strings.ToLower(profileSpec.Name), "pulse") {
			priority = admission.PriorityPulse
		}
		// Recording playback is Live. (Real-time recording intents would be PriorityRecording)

		admitted, reason := s.admission.CanAdmit(r.Context(), priority)
		if !admitted {
			// Record reject metric (Phase 5.3)
			metrics.RecordReject(string(reason), priority.String())

			// ADR-DEGRADATION: Return 503 Service Unavailable
			w.Header().Set("Retry-After", "5")

			// Condition E: Coarse header for external, detailed for authenticated only
			if p := auth.PrincipalFromContext(r.Context()); p != nil {
				// Authenticated: detailed taxonomy
				w.Header().Set("X-Admission-Factor", string(reason))
			} else {
				// External: coarse only
				w.Header().Set("X-Admission-Factor", "capacity-full")
			}

			// Always log detailed reason with request context for operators
			log.L().Info().
				Str("serviceRef", req.ServiceRef).
				Str("reason", string(reason)).
				Int("priority", int(priority)).
				Msg("admission rejected")

			RecordV3Intent(string(model.IntentTypeStreamStart), "admission", string(reason))
			RespondError(w, r, http.StatusServiceUnavailable, &APIError{
				Code:    "ADMISSION_REJECTED",
				Message: "service saturated",
			})
			return
		}

		// Record admit metric (Phase 5.3)
		metrics.RecordAdmit(priority.String())

		var hwaccelEffective, hwaccelReason, encoderBackend string

		if profileSpec.TranscodeVideo {
			if profileSpec.HWAccel == "vaapi" {
				hwaccelEffective = "gpu"
				encoderBackend = "vaapi"
				if hwaccelMode == profiles.HWAccelForce {
					hwaccelReason = "forced"
				} else {
					hwaccelReason = "auto_has_gpu"
				}
			} else {
				hwaccelEffective = "cpu"
				encoderBackend = profileSpec.VideoCodec // libx264, hevc, etc
				if hwaccelMode == profiles.HWAccelOff {
					hwaccelReason = "user_disabled"
				} else if !hasGPU {
					hwaccelReason = "no_gpu_available"
				} else {
					hwaccelReason = "profile_cpu_only"
				}
			}
		} else {
			// Passthrough (no transcoding)
			hwaccelEffective = "off"
			hwaccelReason = "passthrough"
			encoderBackend = "none"
		}

		// Recalculate bucket for idempotency consistency in this scope
		bucket := "0"
		if req.StartMs != nil && *req.StartMs > 0 {
			bucket = fmt.Sprintf("%d", *req.StartMs/1000)
		}

		logger.Info().
			Str("ua", r.UserAgent()).
			Str("profile", profileSpec.Name).
			Int("dvr_window_sec", profileSpec.DVRWindowSec).
			Str("idem_key", idempotencyKey).
			Bool("gpu_available", hasGPU).
			Str("hwaccel_requested", string(hwaccelMode)).
			Str("hwaccel_effective", hwaccelEffective).
			Str("hwaccel_reason", hwaccelReason).
			Str("encoder_backend", encoderBackend).
			Str("video_codec", profileSpec.VideoCodec).
			Str("container", profileSpec.Container).
			Bool("llhls", profileSpec.LLHLS).
			Msg("intent profile resolved")

		if len(cfg.Engine.TunerSlots) == 0 {
			RecordV3Intent(string(model.IntentTypeStreamStart), "phase0", "no_slots")
			w.Header().Set("Retry-After", "10")
			RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, "no tuner slots configured")
			return
		}

		phaseLabel := "phase2"

		// 4. Persistence (Atomic Idempotency)
		// We use PutSessionWithIdempotency to guarantee single-winner for parallel events.
		requestParams := map[string]string{
			"profile": reqProfileID,
			"bucket":  bucket,
		}
		if correlationID != "" {
			requestParams["correlationId"] = correlationID
		}
		if mode != "" {
			requestParams[model.CtxKeyMode] = mode
		}

		// Create Session Record (Starting state)
		session := &model.SessionRecord{
			SessionID:     sessionID,
			State:         model.SessionNew,
			ServiceRef:    req.ServiceRef,
			Profile:       profileSpec,
			CorrelationID: correlationID,
			CreatedAtUnix: time.Now().Unix(),
			UpdatedAtUnix: time.Now().Unix(),
			// ADR-009: Session Lease (config-driven, CTO Patch 1 compliant)
			LeaseExpiresAtUnix: time.Now().Add(cfg.Sessions.LeaseTTL).Unix(),
			HeartbeatInterval:  int(cfg.Sessions.HeartbeatInterval.Seconds()),
			ContextData:        requestParams, // Store context params
		}

		// Atomic Write
		existingID, exists, err := store.PutSessionWithIdempotency(r.Context(), session, idempotencyKey, admissionLeaseTTL)
		if err != nil {
			logger.Error().Err(err).Msg("failed to persist intent")
			RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to persist intent")
			return
		}

		if exists {
			// Idempotent Replay
			RecordV3Replay(string(model.IntentTypeStreamStart))
			RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "replay")

			logger.Info().Str("existing_sid", existingID).Msg("idempotent replay detected")

			// Fetch existing session to get its correlation ID (Hygiene #2)
			existingSession, err := store.GetSession(r.Context(), existingID)
			replayCorrelation := correlationID // Default to current if fetch fails
			if err == nil && existingSession != nil && existingSession.CorrelationID != "" {
				replayCorrelation = existingSession.CorrelationID
			} else if err == nil && existingSession != nil && existingSession.ContextData != nil {
				if cid := existingSession.ContextData["correlationId"]; cid != "" {
					replayCorrelation = cid
				}
			}

			writeJSON(w, http.StatusAccepted, &v3api.IntentResponse{
				SessionID:     existingID,
				Status:        "idempotent_replay",
				CorrelationID: replayCorrelation,
			})
			return
		}

		// 5. Publish Event (Only if new)
		evt := model.StartSessionEvent{
			Type:          model.EventStartSession,
			SessionID:     sessionID,
			ServiceRef:    req.ServiceRef,
			ProfileID:     reqProfileID,
			CorrelationID: correlationID,
			RequestedAtUN: time.Now().Unix(),
		}
		if req.StartMs != nil {
			evt.StartMs = *req.StartMs
		}

		if err := bus.Publish(r.Context(), string(model.EventStartSession), evt); err != nil {
			logger.Error().Err(err).Msg("failed to publish start event")
			RecordV3Publish("session.start", "error")
			RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "publish_error")
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to publish event")
			return
		}
		RecordV3Publish("session.start", "ok")

		// 6. Response
		logger.Info().Msg("intent accepted")
		RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "accepted")
		writeJSON(w, http.StatusAccepted, &v3api.IntentResponse{
			SessionID:     sessionID,
			Status:        "accepted",
			CorrelationID: correlationID,
		})

	case model.IntentTypeStreamStop:
		event := model.StopSessionEvent{
			Type:          model.EventStopSession,
			SessionID:     sessionID,
			Reason:        model.RClientStop,
			CorrelationID: correlationID,
			RequestedAtUN: time.Now().Unix(),
		}
		if err := bus.Publish(r.Context(), string(model.EventStopSession), event); err != nil {
			logger.Error().Err(err).Msg("failed to publish stop event")
			RecordV3Publish("session.stop", "error")
			RecordV3Intent(string(model.IntentTypeStreamStop), "any", "publish_error")
			RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to dispatch intent")
			return
		}
		RecordV3Publish("session.stop", "ok")
		RecordV3Intent(string(model.IntentTypeStreamStop), "any", "accepted")

		writeJSON(w, http.StatusAccepted, &v3api.IntentResponse{
			SessionID:     sessionID,
			Status:        "accepted",
			CorrelationID: correlationID,
		})

	default:
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "unsupported intent type")
	}
}

// ComputeIdemKey generates a deterministic SHA256 idempotency key.
// It uses the canonical payload: "v1:<type>:<ref>:<profile>:<bucket>"
// Secret is no longer used (Server-Side generation is inherently protected).
func ComputeIdemKey(intentType model.IntentType, ref, profile, bucket string) string {
	payload := fmt.Sprintf("v1:%s:%s:%s:%s", intentType, ref, profile, bucket)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}
