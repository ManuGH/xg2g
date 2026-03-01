// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/control/admission"
	"github.com/ManuGH/xg2g/internal/control/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

// Responsibility: Handles Intent creation (Start/Stop stream signals).
// Non-goals: Providing stream data or session status.

type intentDispatchState struct {
	deps          sessionsModuleDeps
	req           v3api.IntentRequest
	intentType    model.IntentType
	sessionID     string
	correlationID string
	mode          string
}

type intentHandlerFunc func(*Server, http.ResponseWriter, *http.Request, *intentDispatchState)

var intentHandlers = map[model.IntentType]intentHandlerFunc{
	model.IntentTypeStreamStart: (*Server).handleV3IntentStart,
	model.IntentTypeStreamStop:  (*Server).handleV3IntentStop,
}

// handleV3Intents handles POST /api/v3/intents.
func (s *Server) handleV3Intents(w http.ResponseWriter, r *http.Request) {
	// 0. Hardening: Limit Request Size (1MB)
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	deps := s.sessionsModuleDeps()
	if deps.bus == nil || deps.store == nil {
		// V3 Worker not running
		respondIntentFailure(w, r, IntentErrV3Unavailable)
		return
	}

	state, ok := s.prepareIntentDispatchState(w, r, deps)
	if !ok {
		return
	}

	handler, ok := intentHandlerForType(state.intentType)
	if !ok {
		respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported intent type")
		return
	}

	handler(s, w, r, state)
}

func intentHandlerForType(intentType model.IntentType) (intentHandlerFunc, bool) {
	handler, ok := intentHandlers[intentType]
	return handler, ok
}

func (s *Server) prepareIntentDispatchState(w http.ResponseWriter, r *http.Request, deps sessionsModuleDeps) (*intentDispatchState, bool) {
	cfg := deps.cfg
	store := deps.store

	var req v3api.IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // Hardening
	if err := dec.Decode(&req); err != nil {
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
		return nil, false
	}
	correlationID, err := v3api.NormalizeCorrelationID(req.CorrelationID)
	if err != nil {
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
		return nil, false
	}

	intentType := req.Type
	if intentType == "" {
		intentType = model.IntentTypeStreamStart
	}

	if intentType == model.IntentTypeStreamStart {
		if u, ok := platformnet.ParseDirectHTTPURL(req.ServiceRef); ok {
			normalized, validateErr := platformnet.ValidateOutboundURL(r.Context(), u.String(), outboundPolicyFromConfig(cfg))
			if validateErr != nil {
				respondIntentFailure(w, r, IntentErrInvalidInput, "direct URL serviceRef rejected by outbound policy")
				return nil, false
			}
			req.ServiceRef = normalized
		}
	}

	var sessionID string
	switch intentType {
	case model.IntentTypeStreamStart:
		sessionID = uuid.New().String()
		if correlationID == "" {
			correlationID = uuid.New().String()
		}
	case model.IntentTypeStreamStop:
		if req.SessionID == "" {
			respondIntentFailure(w, r, IntentErrInvalidInput, "sessionId required for stop")
			return nil, false
		}
		sessionID = req.SessionID
		if correlationID == "" {
			if session, getErr := store.GetSession(r.Context(), sessionID); getErr == nil && session != nil {
				correlationID = session.CorrelationID
			}
		}
	default:
		respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported intent type")
		return nil, false
	}

	mode := model.ModeLive
	modeRecording := normalize.Token(model.ModeRecording)
	modeLive := normalize.Token(model.ModeLive)
	if raw := normalize.Token(req.Params["mode"]); raw != "" {
		if raw == modeRecording {
			respondIntentFailure(w, r, IntentErrInvalidInput, "recording playback uses /recordings")
			return nil, false
		}
		if raw != modeLive {
			respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported playback mode")
			return nil, false
		}
	}

	return &intentDispatchState{
		deps:          deps,
		req:           req,
		intentType:    intentType,
		sessionID:     sessionID,
		correlationID: correlationID,
		mode:          mode,
	}, true
}

func (s *Server) handleV3IntentStart(w http.ResponseWriter, r *http.Request, state *intentDispatchState) {
	deps := state.deps
	req := state.req

	// 5.0 Preflight Source Check (fail-closed)
	if enforcePreflight(r.Context(), w, r, deps, req.ServiceRef) {
		return
	}

	principalID := ""
	if p := auth.PrincipalFromContext(r.Context()); p != nil {
		principalID = p.ID
	}
	logger := log.WithComponentFromContext(r.Context(), "api")
	if state.correlationID != "" {
		logger = logger.With().Str("correlationId", state.correlationID).Logger()
	}

	result, intentErr := s.intentProcessor().ProcessIntent(r.Context(), v3intents.Intent{
		Type:          state.intentType,
		SessionID:     state.sessionID,
		ServiceRef:    req.ServiceRef,
		Params:        req.Params,
		StartMs:       req.StartMs,
		CorrelationID: state.correlationID,
		Mode:          state.mode,
		UserAgent:     r.UserAgent(),
		PrincipalID:   principalID,
		Logger:        logger,
	})
	if intentErr != nil {
		s.writeIntentServiceError(w, r, intentErr)
		return
	}

	if deps.playbackSLO != nil && result.Status == "accepted" {
		modeLabel := playbackModeLabelFromIntentPlaybackMode(req.Params["playback_mode"])
		deps.playbackSLO.Start(playbackSessionMeta{
			SessionID:  result.SessionID,
			Schema:     playbackSchemaLiveLabel,
			Mode:       modeLabel,
			ServiceRef: req.ServiceRef,
		})
		log.L().Debug().
			Str("event", "playback.slo.start").
			Str("request_id", requestID(r.Context())).
			Str("session_id", result.SessionID).
			Str("schema", playbackSchemaLiveLabel).
			Str("mode", modeLabel).
			Str("service_ref", req.ServiceRef).
			Msg("live playback start tracked")
	}

	writeJSON(w, http.StatusAccepted, &v3api.IntentResponse{
		SessionID:     result.SessionID,
		Status:        result.Status,
		CorrelationID: result.CorrelationID,
	})
}

func (s *Server) handleV3IntentStop(w http.ResponseWriter, r *http.Request, state *intentDispatchState) {
	deps := state.deps
	logger := log.WithComponentFromContext(r.Context(), "api")
	if state.correlationID != "" {
		logger = logger.With().Str("correlationId", state.correlationID).Logger()
	}

	result, intentErr := s.intentProcessor().ProcessIntent(r.Context(), v3intents.Intent{
		Type:          state.intentType,
		SessionID:     state.sessionID,
		CorrelationID: state.correlationID,
		Logger:        logger,
	})
	if intentErr != nil {
		s.writeIntentServiceError(w, r, intentErr)
		return
	}

	if deps.playbackSLO != nil && result.Status == "accepted" {
		outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
			SessionID: result.SessionID,
			Schema:    playbackSchemaLiveLabel,
		}, "aborted")
		if outcome.TTFFObserved {
			evt := log.L().Info().
				Str("event", "playback.slo.ttff").
				Str("request_id", requestID(r.Context())).
				Str("session_id", result.SessionID).
				Str("schema", outcome.Schema).
				Str("mode", outcome.Mode).
				Str("outcome", outcome.Outcome).
				Float64("ttff_seconds", outcome.TTFFSeconds)
			if outcome.ServiceRef != "" {
				evt = evt.Str("service_ref", outcome.ServiceRef)
			}
			evt.Msg("live playback ttff outcome observed")
		}
	}

	writeJSON(w, http.StatusAccepted, &v3api.IntentResponse{
		SessionID:     result.SessionID,
		Status:        result.Status,
		CorrelationID: result.CorrelationID,
	})
}

func (s *Server) writeIntentServiceError(w http.ResponseWriter, r *http.Request, err *v3intents.Error) {
	if err == nil {
		respondIntentFailure(w, r, IntentErrInternal)
		return
	}

	switch err.Kind {
	case v3intents.ErrorInvalidInput:
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Message)
	case v3intents.ErrorAdmissionUnavailable:
		writeProblem(w, r, http.StatusServiceUnavailable, "admission/unavailable", "Admission Unavailable", "ADMISSION_UNAVAILABLE", "admission controller unavailable", nil)
	case v3intents.ErrorAdmissionRejected:
		if err.RetryAfter != "" {
			w.Header().Set("Retry-After", err.RetryAfter)
		}
		if err.AdmissionProblem != nil {
			admission.WriteProblem(w, r, err.AdmissionProblem)
			return
		}
		respondIntentFailure(w, r, IntentErrAdmissionUnknown, "admission rejected")
	case v3intents.ErrorNoTunerSlots:
		if err.RetryAfter != "" {
			w.Header().Set("Retry-After", err.RetryAfter)
		}
		respondIntentFailure(w, r, IntentErrNoTunerSlots, err.Message)
	case v3intents.ErrorStoreUnavailable:
		respondIntentFailure(w, r, IntentErrStoreUnavailable, err.Message)
	case v3intents.ErrorPublishUnavailable:
		respondIntentFailure(w, r, IntentErrPublishUnavailable, err.Message)
	default:
		respondIntentFailure(w, r, IntentErrInternal)
	}
}

// ComputeIdemKey generates a deterministic SHA256 idempotency key.
func ComputeIdemKey(intentType model.IntentType, ref, profile, bucket string) string {
	return v3intents.ComputeIdemKey(intentType, ref, profile, bucket)
}
