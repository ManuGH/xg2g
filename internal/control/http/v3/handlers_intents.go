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
	"time"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/domain/session/lifecycle"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
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

	deps := s.sessionsModuleDeps()
	cfg := deps.cfg
	bus := deps.bus
	store := deps.store

	if bus == nil || store == nil {
		// V3 Worker not running
		respondIntentFailure(w, r, IntentErrV3Unavailable)
		return
	}

	// 2. Decode Request
	var req v3api.IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // Hardening
	if err := dec.Decode(&req); err != nil {
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
		return
	}
	correlationID, err := v3api.NormalizeCorrelationID(req.CorrelationID)
	if err != nil {
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
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

	if intentType == model.IntentTypeStreamStart {
		if u, ok := platformnet.ParseDirectHTTPURL(req.ServiceRef); ok {
			normalized, err := platformnet.ValidateOutboundURL(r.Context(), u.String(), outboundPolicyFromConfig(cfg))
			if err != nil {
				respondIntentFailure(w, r, IntentErrInvalidInput, "direct URL serviceRef rejected by outbound policy")
				return
			}
			req.ServiceRef = normalized
		}
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
			respondIntentFailure(w, r, IntentErrInvalidInput, "sessionId required for stop")
			return
		}
		sessionID = req.SessionID
		if correlationID == "" {
			if session, err := store.GetSession(r.Context(), sessionID); err == nil && session != nil {
				correlationID = session.CorrelationID
			}
		}
	default:
		respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported intent type")
		return
	}

	if !correlationProvided && correlationID != "" {
		logger = logger.With().Str("correlationId", correlationID).Logger()
	}

	mode := model.ModeLive
	modeRecording := normalize.Token(model.ModeRecording)
	modeLive := normalize.Token(model.ModeLive)
	if raw := normalize.Token(req.Params["mode"]); raw != "" {
		if raw == modeRecording {
			respondIntentFailure(w, r, IntentErrInvalidInput, "recording playback uses /recordings")
			return
		}
		if raw != modeLive {
			respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported playback mode")
			return
		}
	}

	// 5. Build & Publish Event
	switch intentType {
	case model.IntentTypeStreamStart:
		// Smart Profile Lookup
		var cap *scan.Capability
		if deps.channelScanner != nil {
			if c, found := deps.channelScanner.GetCapability(req.ServiceRef); found {
				cap = &c
			}
		}

		hasGPU := hardware.IsVAAPIReady()
		av1OK := hardware.IsVAAPIEncoderReady("av1_vaapi")
		hevcOK := hardware.IsVAAPIEncoderReady("hevc_vaapi")
		h264OK := hardware.IsVAAPIEncoderReady("h264_vaapi")

		// Parse hwaccel parameter (v3.1+)
		hwaccelMode := profiles.HWAccelAuto // Default
		if hwaccel := normalize.Token(req.Params["hwaccel"]); hwaccel != "" {
			switch hwaccel {
			case "force":
				hwaccelMode = profiles.HWAccelForce
			case "off":
				hwaccelMode = profiles.HWAccelOff
			case "auto":
				hwaccelMode = profiles.HWAccelAuto
			default:
				// Strict validation: unknown hwaccel → 400 Bad Request
				RecordV3Intent(string(model.IntentTypeStreamStart), "phase0", "invalid_hwaccel")
				respondIntentFailure(w, r, IntentErrInvalidInput,
					fmt.Sprintf("invalid hwaccel value: %q (must be auto, force, or off)", hwaccel))
				return
			}
		}

		// Hard-fail if force requested but GPU not verified
		if hwaccelMode == profiles.HWAccelForce && !hasGPU {
			reason := "no /dev/dri/renderD128"
			if hardware.HasVAAPI() {
				reason = "VAAPI preflight encode test failed"
			}
			RecordV3Intent(string(model.IntentTypeStreamStart), "phase0", "hwaccel_unavailable")
			respondIntentFailure(w, r, IntentErrInvalidInput,
				fmt.Sprintf("hwaccel=force requested but GPU not available (%s)", reason))
			return
		}

		// Resolve Profile ID (Testing override allowed via params).
		// When profile isn't explicitly specified, honor client codec preferences
		// (e.g. "av1,hevc,h264") to pick the best output profile.
		reqProfileID := "universal"
		if p := normalize.Token(req.Params["profile"]); p != "" {
			reqProfileID = p
		} else if picked := pickProfileForCodecs(req.Params["codecs"], av1OK, hevcOK, h264OK, hwaccelMode); picked != "" {
			reqProfileID = picked
		}

		// Bucket: 0 for Live, StartTime/1000 for VOD (1 second resolution).
		bucket := "0"
		if req.StartMs != nil && *req.StartMs > 0 {
			bucket = fmt.Sprintf("%d", *req.StartMs/1000)
		}

		// Compute Idempotency Key (Server-Side) after final profile selection.
		idempotencyKey := ComputeIdemKey(model.IntentTypeStreamStart, req.ServiceRef, reqProfileID, bucket)

		// Resolve() uses a single hasGPU boolean to decide whether VAAPI is eligible.
		// For codec-specific profiles (AV1/HEVC/H264), we only pass hasGPU=true when
		// the corresponding encoder was verified by VAAPI preflight.
		resolveHasGPU := hasGPU
		switch reqProfileID {
		case profiles.ProfileAV1HW:
			resolveHasGPU = av1OK
		case profiles.ProfileSafariHEVCHW:
			resolveHasGPU = hevcOK
		case profiles.ProfileH264FMP4:
			resolveHasGPU = h264OK
		}
		profileSpec := profiles.Resolve(reqProfileID, r.UserAgent(), int(cfg.HLS.DVRWindow.Seconds()), cap, resolveHasGPU, hwaccelMode)

		// 5.0 Preflight Source Check (fail-closed)
		if enforcePreflight(r.Context(), w, r, deps, req.ServiceRef) {
			return
		}

		// 5.1 Admission Control Gate (Slice 2)
		state := CollectRuntimeState(r.Context(), deps.admissionState)
		wantsTranscode := profileSpec.TranscodeVideo // Profile knows if transcode is needed

		if deps.admission == nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "admission/unavailable", "Admission Unavailable", "ADMISSION_UNAVAILABLE", "admission controller unavailable", nil)
			return
		}

		decision := deps.admission.Check(r.Context(), admission.Request{WantsTranscode: wantsTranscode}, state)
		if !decision.Allow {
			// Record reject metric (Slice 2)
			if decision.Problem != nil {
				metrics.RecordReject(decision.Problem.Code, "live") // Simplified priority for now
			}

			// Add Retry-After if applicable
			if decision.RetryAfterSeconds != nil {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", *decision.RetryAfterSeconds))
			} else if decision.Problem != nil && (decision.Problem.Code == admission.CodeNoTuners || decision.Problem.Code == admission.CodeSessionsFull) {
				// Deterministic overrides if controller didn't spec it (though controller should?)
				// For Slice 2, let's stick to Problem.
				w.Header().Set("Retry-After", "5") // Safe default for capacity
			}

			// Log rejection
			log.L().Info().
				Str("serviceRef", req.ServiceRef).
				Str("code", decision.Problem.Code).
				Msg("admission rejected")

			RecordV3Intent(string(model.IntentTypeStreamStart), "admission", decision.Problem.Code)
			admission.WriteProblem(w, r, decision.Problem)
			return
		}

		// Record admit metric (Phase 5.3)
		// Record admit metric (Phase 5.3)
		metrics.RecordAdmit("live") // Simplified for Slice 2

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
			respondIntentFailure(w, r, IntentErrNoTunerSlots, "no tuner slots configured")
			return
		}

		phaseLabel := "phase2"

		// 4. Persistence (Atomic Idempotency)
		// We use PutSessionWithIdempotency to guarantee single-winner for parallel events.
		requestParams := map[string]string{
			"profile": reqProfileID,
			"bucket":  bucket,
		}
		if raw := req.Params["codecs"]; raw != "" {
			requestParams["codecs"] = raw
		}
		if correlationID != "" {
			requestParams["correlationId"] = correlationID
		}
		if mode != "" {
			requestParams[model.CtxKeyMode] = mode
		}

		// Create Session Record (Starting state)
		session := lifecycle.NewSessionRecord(time.Now())
		session.SessionID = sessionID
		session.ServiceRef = req.ServiceRef
		session.Profile = profileSpec
		session.CorrelationID = correlationID
		// ADR-009: Session Lease (config-driven, CTO Patch 1 compliant)
		session.LeaseExpiresAtUnix = time.Now().Add(cfg.Sessions.LeaseTTL).Unix()
		session.HeartbeatInterval = int(cfg.Sessions.HeartbeatInterval.Seconds())
		session.ContextData = requestParams // Store context params

		// Atomic Write
		existingID, exists, err := store.PutSessionWithIdempotency(r.Context(), session, idempotencyKey, admissionLeaseTTL)
		if err != nil {
			logger.Error().Err(err).Msg("failed to persist intent")
			RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "store_error")
			respondIntentFailure(w, r, IntentErrStoreUnavailable, "failed to persist intent")
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
			respondIntentFailure(w, r, IntentErrPublishUnavailable, "failed to publish event")
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
			respondIntentFailure(w, r, IntentErrPublishUnavailable, "failed to dispatch intent")
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
		respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported intent type")
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
