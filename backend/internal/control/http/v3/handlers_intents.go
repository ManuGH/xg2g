// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/ManuGH/xg2g/internal/control/admission"
	controlauth "github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// Responsibility: Handles Intent creation (Start/Stop stream signals).
// Non-goals: Providing stream data or session status.

// handleV3Intents handles POST /api/v3/intents.
func (s *Server) handleV3Intents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	deps := s.sessionsModuleDeps()
	cfg := deps.cfg
	bus := deps.bus
	store := deps.store

	if bus == nil || store == nil {
		respondIntentFailure(w, r, IntentErrV3Unavailable)
		return
	}

	var req v3api.IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
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
	decisionTraceID := ""
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

	if intentType == model.IntentTypeStreamStart {
		if req.PlaybackDecisionToken == nil || *req.PlaybackDecisionToken == "" {
			writeRegisteredProblem(w, r, http.StatusUnauthorized, "intent/token-missing", "Missing Decision Token", problemcode.CodeTokenMissing, "A valid playbackDecisionToken is required to start a live stream", nil)
			return
		}

		s.mu.RLock()
		jwtSecret := append([]byte(nil), s.JWTSecret...)
		s.mu.RUnlock()
		if len(jwtSecret) == 0 {
			writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "intent/security-unavailable", "Security Unavailable", problemcode.CodeSecurityUnavailable, "Decision token verification is not configured", nil)
			return
		}

		claims, err := auth.VerifyStrict(*req.PlaybackDecisionToken, jwtSecret, "xg2g/v3/intents", "xg2g")
		if err != nil {
			code := auth.ClassifyError(err)
			writeRegisteredProblem(w, r, http.StatusUnauthorized, "intent/unauthorized", "Unauthorized Intent", code, err.Error(), nil)
			return
		}
		if claims.TraceID != "" {
			decisionTraceID = claims.TraceID
			logger = logger.With().Str("traceId", claims.TraceID).Logger()
		}

		normRef := normalize.ServiceRef(req.ServiceRef)
		normTokenSub := normalize.ServiceRef(claims.Sub)
		if normRef != normTokenSub {
			writeRegisteredProblem(w, r, http.StatusForbidden, "intent/claim-mismatch", "Forbidden Action", problemcode.CodeClaimMismatch, "Token is not authorized for this service_ref", nil)
			return
		}

		var reqMode string
		if req.Params != nil {
			reqMode = req.Params["mode"]
		}
		if raw := normalize.Token(reqMode); raw != "" && raw != claims.Mode {
			writeRegisteredProblem(w, r, http.StatusForbidden, "intent/claim-mismatch", "Forbidden Action", problemcode.CodeClaimMismatch, "Token is not authorized for this playback mode", nil)
			return
		}

		var expectedHash string
		if req.Params != nil {
			if rawCapHash := normalize.Token(req.Params["capHash"]); rawCapHash != "" {
				expectedHash = rawCapHash
			} else if rawCapHash := normalize.Token(req.Params["cap_hash"]); rawCapHash != "" {
				expectedHash = rawCapHash
			} else {
				genericMap := make(map[string]interface{}, len(req.Params))
				for k, v := range req.Params {
					genericMap[k] = v
				}
				if cHash, err := normalize.MapHash(genericMap); err == nil {
					expectedHash = cHash
				}
			}
		}
		if claims.CapHash != "" && claims.CapHash != expectedHash {
			writeRegisteredProblem(w, r, http.StatusForbidden, "intent/claim-mismatch", "Forbidden Action", problemcode.CodeClaimMismatch, "Token is not authorized for these playback capabilities", nil)
			return
		}

		if enforcePreflight(r.Context(), w, r, deps, req.ServiceRef) {
			return
		}
	}

	principalID := ""
	if principal := controlauth.PrincipalFromContext(r.Context()); principal != nil {
		principalID = principal.ID
	}

	result, intentErr := s.intentProcessor().ProcessIntent(r.Context(), v3intents.Intent{
		Type:          intentType,
		SessionID:     sessionID,
		ServiceRef:    req.ServiceRef,
		Params:        req.Params,
		StartMs:       req.StartMs,
		CorrelationID: correlationID,
		DecisionTrace: decisionTraceID,
		Mode:          mode,
		UserAgent:     r.UserAgent(),
		PrincipalID:   principalID,
		Logger:        logger,
	})
	if intentErr != nil {
		writeIntentProcessingError(w, r, intentErr)
		return
	}
	if result == nil {
		respondIntentFailure(w, r, IntentErrInternal, "intent service returned no result")
		return
	}

	writeJSON(w, http.StatusAccepted, &v3api.IntentResponse{
		SessionID:     result.SessionID,
		Status:        result.Status,
		CorrelationID: result.CorrelationID,
	})
}

func (s *Server) intentProcessor() *v3intents.Service {
	s.mu.RLock()
	if s.intentService != nil {
		defer s.mu.RUnlock()
		return s.intentService
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intentService == nil {
		s.intentService = v3intents.NewService(&serverIntentDeps{s: s})
	}
	return s.intentService
}

func writeIntentProcessingError(w http.ResponseWriter, r *http.Request, err *v3intents.Error) {
	if err == nil {
		respondIntentFailure(w, r, IntentErrInternal)
		return
	}

	switch err.Kind {
	case v3intents.ErrorInvalidInput:
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
	case v3intents.ErrorAdmissionUnavailable:
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "admission/unavailable", "Admission Unavailable", problemcode.CodeAdmissionUnavailable, "admission controller unavailable", nil)
	case v3intents.ErrorAdmissionRejected:
		if err.RetryAfter != "" {
			w.Header().Set("Retry-After", err.RetryAfter)
		}
		if err.AdmissionProblem != nil {
			admission.WriteProblem(w, r, err.AdmissionProblem)
			return
		}
		respondIntentFailure(w, r, IntentErrAdmissionUnknown, err.Error())
	case v3intents.ErrorNoTunerSlots:
		if err.RetryAfter != "" {
			w.Header().Set("Retry-After", err.RetryAfter)
		}
		respondIntentFailure(w, r, IntentErrNoTunerSlots, err.Error())
	case v3intents.ErrorStoreUnavailable:
		respondIntentFailure(w, r, IntentErrStoreUnavailable, err.Error())
	case v3intents.ErrorPublishUnavailable:
		respondIntentFailure(w, r, IntentErrPublishUnavailable, err.Error())
	default:
		respondIntentFailure(w, r, IntentErrInternal, err.Error())
	}
}

// ComputeIdemKey generates a deterministic SHA256 idempotency key.
// It uses the canonical payload: "v1:<type>:<ref>:<profile>:<bucket>".
func ComputeIdemKey(intentType model.IntentType, ref, profile, bucket string) string {
	return v3intents.ComputeIdemKey(intentType, ref, profile, bucket)
}
