// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ManuGH/xg2g/internal/log"
	v3api "github.com/ManuGH/xg2g/internal/v3/api"
	"github.com/ManuGH/xg2g/internal/v3/hardware"
	"github.com/ManuGH/xg2g/internal/v3/lease"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/profiles"
	"github.com/ManuGH/xg2g/internal/v3/scan"
	v3store "github.com/ManuGH/xg2g/internal/v3/store"
)

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
		logger = logger.With().Str("correlation_id", correlationID).Logger()
	}

	intentType := req.Type
	if intentType == "" {
		intentType = model.IntentTypeStreamStart
	}

	// 3. Compute Idempotency Key (Server-Side)
	// Policy: We calculate this strictly based on inputs to prevent client spoofing.
	// If the client provided a key, we might validate it, but we prefer stable generation.
	// Users with same ServiceRef + Profile + Bucket get same Key.
	var idempotencyKey string
	if intentType == model.IntentTypeStreamStart {
		// Canonicalize profile first
		requestedProfile, legacyProfile, err := canonicalProfileID(req.ProfileID, req.Profile)
		if err != nil {
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Error())
			return
		}
		if legacyProfile {
			logger.Info().Msg("legacy field 'profile' used; will be removed in v3.2")
		}

		// Bucket: 0 for Live, StartTime/1000 for VOD
		bucket := "0"
		if req.StartMs != nil && *req.StartMs > 0 {
			// VOD/Catchup bucket (1 second resolution)
			bucket = fmt.Sprintf("%d", *req.StartMs/1000)
		}

		// Compute Usage Key
		idempotencyKey = ComputeIdemKey(model.IntentTypeStreamStart, req.ServiceRef, requestedProfile, bucket)

		// Check if we should rotate/verify (Optional/Advanced: not strictly needed for creation, only for verification if we supported client keys)
		// For creation, we always sign with Current.
	}

	// 4. Generate Session ID (Strong UUID)
	// For START, new ID. For STOP, use provided ID.
	var sessionID string
	if intentType == model.IntentTypeStreamStart {
		// New Session
		sessionID = uuid.New().String()
		if correlationID == "" {
			correlationID = uuid.New().String()
		}
	} else if intentType == model.IntentTypeStreamStop {
		// STOP logic remains same
		if req.SessionID == "" {
			RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "sessionId required for stop")
			return
		}
		sessionID = req.SessionID
		// ... retrieve correlation if needed ...
		if correlationID == "" {
			if session, err := store.GetSession(r.Context(), sessionID); err == nil && session != nil {
				correlationID = session.CorrelationID
			}
		}
	} else {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "unsupported intent type")
		return
	}

	// Add correlation to logger
	if !correlationProvided && correlationID != "" {
		logger = logger.With().Str("correlation_id", correlationID).Logger()
	}

	// Extract ClientIP (Normalized)
	clientIP := req.Params["client_ip"]
	if clientIP == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			clientIP = host
		} else {
			clientIP = r.RemoteAddr
		}
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

	var acquiredLeases []v3store.Lease
	releaseLeases := func() {
		if len(acquiredLeases) == 0 {
			return
		}
		ctxRel, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, l := range acquiredLeases {
			_ = store.ReleaseLease(ctxRel, l.Key(), l.Owner())
		}
	}

	// 5. Build & Publish Event
	switch intentType {
	case model.IntentTypeStreamStart:
		// Re-resolve profileSpec to get details (Name, etc)
		// Note: canonicalProfileID was called above, so we know it's valid
		reqProfileID, _, _ := canonicalProfileID(req.ProfileID, req.Profile)

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

		profileSpec := profiles.Resolve(reqProfileID, r.UserAgent(), cfg.DVRWindowSec, cap, hasGPU, hwaccelMode)

		// Determine effective hwaccel outcome (for deterministic logging)
		hwaccelEffective := "cpu"
		hwaccelReason := "not_applicable"
		encoderBackend := "sw"

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

		if len(cfg.TunerSlots) == 0 {
			RecordV3Intent(string(model.IntentTypeStreamStart), "phase0", "no_slots")
			RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, "no tuner slots configured")
			return
		}

		// MIGRATION CONTRACT (ADR-003):
		// Phase 1 (Legacy): V3APILeases=true
		// Phase 2 (Target): V3APILeases=false
		phaseLabel := "phase2"
		if cfg.V3APILeases {
			phaseLabel = "phase1"
			dedupKey := lease.LeaseKeyService(req.ServiceRef)
			dedupLease, ok, err := store.TryAcquireLease(r.Context(), dedupKey, sessionID, admissionLeaseTTL)
			if err != nil {
				RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "lease_error")
				RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, "lease acquisition failed")
				return
			}
			if !ok {
				RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "conflict")
				w.Header().Set("Retry-After", strconv.Itoa(admissionRetryAfterSec))
				RespondError(w, r, http.StatusConflict, ErrLeaseBusy)
				return
			}
			acquiredLeases = append(acquiredLeases, dedupLease)

			_, tunerLease, ok, err := tryAcquireTunerLease(r.Context(), store, sessionID, cfg.TunerSlots, admissionLeaseTTL)
			if err != nil {
				releaseLeases()
				RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "tuner_error")
				RespondError(w, r, http.StatusServiceUnavailable, ErrServiceUnavailable, "tuner lease acquisition failed")
				return
			}
			if !ok {
				releaseLeases()
				RecordV3Intent(string(model.IntentTypeStreamStart), phaseLabel, "conflict")
				w.Header().Set("Retry-After", strconv.Itoa(admissionRetryAfterSec))
				RespondError(w, r, http.StatusConflict, ErrLeaseBusy)
				return
			}
			acquiredLeases = append(acquiredLeases, tunerLease)
		}

		// 4. Persistence (Atomic Idempotency)
		// We use PutSessionWithIdempotency to guarantee single-winner for parallel events.
		requestParams := map[string]string{
			"profile": reqProfileID,
			"bucket":  bucket,
		}
		if correlationID != "" {
			requestParams["correlation_id"] = correlationID
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
			ContextData:   requestParams, // Store context params
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
				if cid := existingSession.ContextData["correlation_id"]; cid != "" {
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
			// If publish fails, we should probably delete the session?
			// Or let it expire. Expiry is safer.
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
		// For STOP, we don't create a session record if it doesn't exist.
		// We just publish the stop event. The worker/orchestrator handles the rest.
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
		// Other intent types...
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "unsupported intent type")
	}
}

// handleV3SessionsDebug dumps all sessions from the store (Admin only).
// Authorization: Requires v3:admin scope (enforced by route middleware).
// Supports pagination via query parameters: offset (default 0) and limit (default 100, max 1000).
func (s *Server) handleV3SessionsDebug(w http.ResponseWriter, r *http.Request) {
	// 1. Access Store
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 store not initialized",
		})
		return
	}

	// 2. Parse Pagination Parameters
	offset, limit := parsePaginationParams(r)

	// 3. List Sessions
	allSessions, err := store.ListSessions(r.Context())
	if err != nil {
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
		return
	}

	// 4. Apply Pagination
	total := len(allSessions)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	sessions := allSessions[start:end]

	// 5. Build Response with Metadata
	response := map[string]any{
		"sessions": sessions,
		"pagination": map[string]int{
			"offset": offset,
			"limit":  limit,
			"total":  total,
			"count":  len(sessions),
		},
	}

	writeJSON(w, http.StatusOK, response)
}

func tryAcquireTunerLease(ctx context.Context, st v3store.StateStore, owner string, slots []int, ttl time.Duration) (slot int, l v3store.Lease, ok bool, err error) {
	for _, s := range slots {
		key := lease.LeaseKeyTunerSlot(s)
		l, got, e := st.TryAcquireLease(ctx, key, owner, ttl)
		if e != nil {
			return 0, nil, false, e
		}
		if got {
			return s, l, true, nil
		}
	}
	return 0, nil, false, nil
}

// handleV3SessionState returns a single session state.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3SessionState(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 store not initialized",
		})
		return
	}

	sessionID := chi.URLParam(r, "sessionID")
	if sessionID == "" || !model.IsSafeSessionID(sessionID) {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid session id")
		return
	}

	session, err := store.GetSession(r.Context(), sessionID)
	if err != nil || session == nil {
		RespondError(w, r, http.StatusNotFound, &APIError{
			Code:    "SESSION_NOT_FOUND",
			Message: "session not found",
		})
		return
	}

	resp := v3api.SessionResponse{
		SessionID:     session.SessionID,
		ServiceRef:    session.ServiceRef,
		Profile:       session.Profile.Name,
		State:         session.State,
		Reason:        session.Reason,
		ReasonDetail:  session.ReasonDetail,
		CorrelationID: session.CorrelationID,
		UpdatedAtMs:   session.UpdatedAtUnix * 1000,
	}
	mode, durationSeconds, seekableStart, seekableEnd, liveEdge := sessionPlaybackInfo(session, time.Now())
	resp.Mode = mode
	resp.DurationSeconds = durationSeconds
	resp.SeekableStartSeconds = seekableStart
	resp.SeekableEndSeconds = seekableEnd
	resp.LiveEdgeSeconds = liveEdge
	resp.PlaybackURL = fmt.Sprintf("/api/v3/sessions/%s/hls/index.m3u8", session.SessionID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleV3HLS serves HLS playlists and segments.
// Authorization: Requires v3:read scope (enforced by route middleware).
func (s *Server) handleV3HLS(w http.ResponseWriter, r *http.Request) {
	// 1. Check v3 availability
	s.mu.RLock()
	store := s.v3Store
	s.mu.RUnlock()

	if store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 not available",
		})
		return
	}

	// 2. Extract Params
	sessionID := chi.URLParam(r, "sessionID")
	filename := chi.URLParam(r, "filename")

	// 3. Serve via HLS helper
	v3api.ServeHLS(w, r, store, s.GetConfig().HLSRoot, sessionID, filename)
}

// parsePaginationParams extracts offset and limit from query parameters.
// Defaults: offset=0, limit=100. Max limit: 1000.
func parsePaginationParams(r *http.Request) (offset int, limit int) {
	// Default values
	offset = 0
	limit = 100

	// Parse offset
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if val, err := strconv.Atoi(offsetStr); err == nil && val >= 0 {
			offset = val
		}
	}

	// Parse limit
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
			if limit > 1000 {
				limit = 1000 // Cap at 1000
			}
		}
	}

	return offset, limit
}

func canonicalProfileID(profileID, profile string) (string, bool, error) {
	if profileID != "" && profile != "" && profileID != profile {
		return "", false, errors.New("profile and profileID must match when both are set")
	}
	if profileID != "" {
		return profileID, false, nil
	}
	if profile != "" {
		return profile, true, nil
	}
	return "", false, nil
}

func sessionPlaybackInfo(session *model.SessionRecord, now time.Time) (string, *float64, *float64, *float64, *float64) {
	mode := model.ModeLive
	if session.ContextData != nil {
		if raw := strings.TrimSpace(session.ContextData[model.CtxKeyMode]); raw != "" {
			mode = strings.ToUpper(raw)
		}
	}
	if mode != model.ModeLive && mode != model.ModeRecording {
		mode = model.ModeLive
	}

	if mode == model.ModeRecording {
		durationSeconds := parseContextSeconds(session.ContextData, model.CtxKeyDurationSeconds)
		if durationSeconds == nil {
			return mode, nil, nil, nil, nil
		}
		zero := 0.0
		return mode, durationSeconds, &zero, durationSeconds, nil
	}

	var durationSeconds *float64
	if session.Profile.DVRWindowSec > 0 {
		val := float64(session.Profile.DVRWindowSec)
		durationSeconds = &val
	}

	nowUnix := session.LastAccessUnix
	if nowUnix == 0 {
		nowUnix = session.UpdatedAtUnix
	}
	if nowUnix == 0 {
		nowUnix = now.Unix()
	}

	startUnix := session.CreatedAtUnix
	if startUnix == 0 {
		startUnix = session.UpdatedAtUnix
	}
	if startUnix == 0 {
		startUnix = nowUnix
	}

	liveEdgeVal := float64(nowUnix - startUnix)
	if liveEdgeVal < 0 {
		liveEdgeVal = 0
	}

	seekableStart := liveEdgeVal
	if durationSeconds != nil && *durationSeconds > 0 {
		seekableStart = liveEdgeVal - *durationSeconds
		if seekableStart < 0 {
			seekableStart = 0
		}
	}

	return mode, durationSeconds, &seekableStart, &liveEdgeVal, &liveEdgeVal
}

func parseContextSeconds(ctx map[string]string, key string) *float64 {
	if ctx == nil {
		return nil
	}
	raw := strings.TrimSpace(ctx[key])
	if raw == "" {
		return nil
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil || val <= 0 {
		return nil
	}
	return &val
}

// ComputeIdemKey generates a deterministic SHA256 idempotency key.
// It uses the canonical payload: "v1:<type>:<ref>:<profile>:<bucket>"
// Secret is no longer used (Server-Side generation is inherently protected).
func ComputeIdemKey(intentType model.IntentType, ref, profile, bucket string) string {
	payload := fmt.Sprintf("v1:%s:%s:%s:%s", intentType, ref, profile, bucket)
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}

// ReportPlaybackFeedback handles POST /sessions/{sessionId}/feedback
func (s *Server) ReportPlaybackFeedback(w http.ResponseWriter, r *http.Request, sessionId openapi_types.UUID) {
	// 1. Verify V3 Available
	s.mu.RLock()
	bus := s.v3Bus
	store := s.v3Store
	s.mu.RUnlock()

	if bus == nil || store == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &APIError{
			Code:    "V3_UNAVAILABLE",
			Message: "v3 not available",
		})
		return
	}

	// 2. Decode Feedback
	var req PlaybackFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "invalid body")
		return
	}

	// 3. Check for MediaError 3 (Safari HLS decode error)
	// We only trigger fallback if it's an error and specifically code 3
	isDecodeError := req.Event == PlaybackFeedbackRequestEventError && req.Code != nil && *req.Code == 3

	if !isDecodeError {
		// Just log info/warnings
		log.L().Info().
			Str("session_id", sessionId.String()).
			Str("event", string(req.Event)).
			Int("code", derefInt(req.Code)).
			Str("msg", derefString(req.Message)).
			Msg("playback feedback received")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// 4. Trigger Fallback
	ctx := r.Context()
	sess, err := store.GetSession(ctx, sessionId.String())
	if err != nil || sess == nil {
		RespondError(w, r, http.StatusNotFound, &APIError{Code: "NOT_FOUND", Message: "session not found"})
		return
	}

	// Atomic Update via Store
	var changed bool
	updatedSess, err := store.UpdateSession(ctx, sessionId.String(), func(s *model.SessionRecord) error {
		// If we are already in Repair profile, nothing more to do
		if s.Profile.Name == profiles.ProfileRepair {
			return nil
		}

		// Force switch to Repair Profile (CPU Transcode, Safe Settings)
		// We manually construct the repair spec as we don't have access to Resolve() deps here easily
		// or we can reuse existing profile but modify it.
		// Existing ProfileRepair definition: Transcode=true, Deinterlace=false, CRF=24, Width=1280

		s.Profile.Name = profiles.ProfileRepair
		s.Profile.TranscodeVideo = true
		s.Profile.Deinterlace = false // Keep simple
		s.Profile.HWAccel = ""        // Force CPU
		s.Profile.VideoCodec = "libx264"
		s.Profile.VideoCRF = 24
		s.Profile.VideoMaxWidth = 1280
		s.Profile.Preset = "veryfast"
		s.Profile.Container = "fmp4" // Ensure FMP4 for Safari

		s.FallbackReason = fmt.Sprintf("client_report:code=%d", derefInt(req.Code))
		s.FallbackAtUnix = time.Now().Unix()
		changed = true
		return nil
	})

	if err != nil {
		log.L().Error().Err(err).Msg("failed to update session for fallback")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "store update failed")
		return
	}

	if !changed {
		log.L().Info().Str("session_id", sessionId.String()).Msg("fallback already active, ignoring request")
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Trigger Restart only if we actually applied the fallback
	sess = updatedSess
	log.L().Warn().Str("session_id", sess.SessionID).Msg("activating safari fallback (fmp4) due to client error")

	// Stop existing session
	stopEvt := model.StopSessionEvent{
		Type:          model.EventStopSession,
		SessionID:     sess.SessionID,
		Reason:        model.RClientStop,
		CorrelationID: sess.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}
	if err := bus.Publish(ctx, string(model.EventStopSession), stopEvt); err != nil {
		log.L().Error().Err(err).Msg("failed to publish stop event during fallback")
	}

	// Start new session
	startEvt := model.StartSessionEvent{
		Type:          model.EventStartSession,
		SessionID:     sess.SessionID,
		ServiceRef:    sess.ServiceRef,
		CorrelationID: sess.CorrelationID,
		RequestedAtUN: time.Now().Unix(),
	}

	if err := bus.Publish(ctx, string(model.EventStartSession), startEvt); err != nil {
		log.L().Error().Err(err).Msg("failed to publish restart event during fallback")
	}

	w.WriteHeader(http.StatusAccepted)
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
