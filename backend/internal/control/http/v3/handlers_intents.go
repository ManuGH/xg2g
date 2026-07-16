// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/ManuGH/xg2g/internal/control/admission"
	controlauth "github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/normalize"
	pipelineapi "github.com/ManuGH/xg2g/internal/pipeline/api"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/ManuGH/xg2g/internal/telemetry"
)

// Responsibility: Handles Intent creation (Start/Stop stream signals).
// Non-goals: Providing stream data or session status.

// handleV3Intents handles POST /api/v3/intents.
func (s *Server) handleV3Intents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	deps := s.sessionsModuleDeps()
	bus := deps.bus
	store := deps.store

	if bus == nil || store == nil {
		respondIntentFailure(w, r, IntentErrV3Unavailable)
		return
	}

	var req IntentRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
		return
	}

	correlationID, err := pipelineapi.NormalizeCorrelationID(derefString(req.CorrelationId))
	if err != nil {
		respondIntentFailure(w, r, IntentErrInvalidInput, err.Error())
		return
	}

	logger := log.WithComponentFromContext(r.Context(), "api")
	correlationProvided := correlationID != ""
	if correlationProvided {
		logger = logger.With().Str("correlationId", correlationID).Logger()
	}

	intentType := model.IntentType(derefStringIntentType(req.Type))
	if intentType == "" {
		intentType = model.IntentTypeStreamStart
	}

	serviceRef, done := resolveIntentServiceRef(w, r, deps, intentType, derefString(req.ServiceRef))
	if done {
		return
	}

	rawPlaybackDecisionToken := derefString(req.PlaybackDecisionToken)
	sessionID, correlationID, done := resolveIntentSession(w, r, store, intentType, req, correlationID)
	if done {
		return
	}

	if intentType == model.IntentTypeStreamStart {
		telemetry.NewStartupTracer(sessionID)
	}

	if !correlationProvided && correlationID != "" {
		logger = logger.With().Str("correlationId", correlationID).Logger()
	}

	mode := model.ModeLive
	modeRecording := normalize.Token(model.ModeRecording)
	modeLive := normalize.Token(model.ModeLive)
	decisionTraceID := ""
	var verifiedReceipt *v3intents.PlanningHandoff
	clientCaps := normalizeIntentClientCaps(req.Client)
	clientCapHash := hashV3Capabilities(req.Client)
	params := normalizeIntentParams(req.Params, clientCaps, clientCapHash)
	if raw := normalize.Token(params["mode"]); raw != "" {
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
		verified, tokenDone := s.verifyDecisionToken(w, r, deps, serviceRef, rawPlaybackDecisionToken, params, clientCapHash)
		if tokenDone {
			return
		}
		verifiedReceipt = verified.Receipt
		if verifiedReceipt != nil {
			if boundSessionID := verifiedReceipt.Receipt.ConsumedSessionID; boundSessionID != "" {
				sessionID = boundSessionID
			}
			consumed, consumeDone := s.consumePlannerReceipt(w, r, verifiedReceipt.Receipt.ReceiptID, sessionID)
			if consumeDone {
				return
			}
			verifiedReceipt = consumed
		}
		if verified.TraceID != "" {
			decisionTraceID = verified.TraceID
			logger = logger.With().Str("traceId", verified.TraceID).Logger()
		}
	}

	principalID := ""
	if principal := controlauth.PrincipalFromContext(r.Context()); principal != nil {
		principalID = principal.ID
	}

	intent := v3intents.Intent{
		Type:                  intentType,
		SessionID:             sessionID,
		ServiceRef:            serviceRef,
		PlaybackDecisionToken: rawPlaybackDecisionToken,
		Params:                params,
		StartMs:               req.StartMs,
		CorrelationID:         correlationID,
		DecisionTrace:         decisionTraceID,
		Mode:                  mode,
		UserAgent:             r.UserAgent(),
		PrincipalID:           principalID,
		ClientCaps:            clientCaps,
		ClientCapHash:         clientCapHash,
		Logger:                logger,
	}
	verifiedReceipt.ApplyTo(&intent)
	result, intentErr := s.intentProcessor().ProcessIntent(r.Context(), intent)
	if intentErr != nil {
		writeIntentProcessingError(w, r, intentErr)
		return
	}
	if result == nil {
		respondIntentFailure(w, r, IntentErrInternal, "intent service returned no result")
		return
	}

	status := IntentAcceptedResponseStatus(result.Status)
	sessionUUID := openapi_types.UUID(parseUUID(result.SessionID))
	resp := IntentAcceptedResponse{
		SessionId: sessionUUID,
		RequestId: effectiveRequestID(w, r.Context()),
		Status:    status,
	}
	if result.CorrelationID != "" {
		resp.CorrelationId = &result.CorrelationID
	}
	writeJSON(w, http.StatusAccepted, &resp)
}

// resolveIntentServiceRef trims the requested serviceRef and, for stream-start
// intents that carry a direct HTTP(S) URL, validates it against the outbound
// policy. It returns the resolved serviceRef and done=true when it has already
// written a failure response and the caller must return.
func resolveIntentServiceRef(w http.ResponseWriter, r *http.Request, deps sessionsModuleDeps, intentType model.IntentType, rawServiceRef string) (serviceRef string, done bool) {
	serviceRef = strings.TrimSpace(rawServiceRef)
	if intentType == model.IntentTypeStreamStart {
		if u, ok := platformnet.ParseDirectHTTPURL(serviceRef); ok {
			normalized, err := platformnet.ValidateOutboundURL(r.Context(), u.String(), outboundPolicyFromConfig(deps.cfg))
			if err != nil {
				respondIntentFailure(w, r, IntentErrInvalidInput, "direct URL serviceRef rejected by outbound policy")
				return "", true
			}
			serviceRef = normalized
		}
	}
	return serviceRef, false
}

// resolveIntentSession derives the sessionID and (where missing) the
// correlationID for the requested intent type. It returns done=true when it has
// already written a failure response and the caller must return.
func resolveIntentSession(w http.ResponseWriter, r *http.Request, store SessionStateStore, intentType model.IntentType, req IntentRequest, correlationID string) (sessionID string, resolvedCorrelationID string, done bool) {
	switch intentType {
	case model.IntentTypeStreamStart:
		sessionID = uuid.New().String()
		if correlationID == "" {
			correlationID = uuid.New().String()
		}
	case model.IntentTypeStreamStop:
		if req.SessionId == nil {
			respondIntentFailure(w, r, IntentErrInvalidInput, "sessionId required for stop")
			return "", "", true
		}
		sessionID = req.SessionId.String()
		if correlationID == "" {
			if session, err := store.GetSession(r.Context(), sessionID); err == nil && session != nil {
				correlationID = session.CorrelationID
			}
		}
	default:
		respondIntentFailure(w, r, IntentErrInvalidInput, "unsupported intent type")
		return "", "", true
	}
	return sessionID, correlationID, false
}

// verifyDecisionToken validates the playbackDecisionToken for a stream-start
// intent: it checks token presence, the configured signing secret, the token
// signature/claims (service_ref, mode, capabilities hash) and the preflight
// gate. It returns the decision trace ID extracted from the token (if any) and
// done=true when it has already written a problem response and the caller must
// return.
type verifiedDecisionToken struct {
	TraceID string
	Receipt *v3intents.PlanningHandoff
}

func (s *Server) verifyDecisionToken(w http.ResponseWriter, r *http.Request, deps sessionsModuleDeps, serviceRef, rawPlaybackDecisionToken string, params map[string]string, clientCapHash string) (verified verifiedDecisionToken, done bool) {
	if strings.TrimSpace(rawPlaybackDecisionToken) == "" {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "intent/token-missing", "Missing Decision Token", problemcode.CodeTokenMissing, "A valid playbackDecisionToken is required to start a live stream", nil)
		return verifiedDecisionToken{}, true
	}

	s.mu.RLock()
	jwtSecret := append([]byte(nil), s.JWTSecret...)
	s.mu.RUnlock()
	if len(jwtSecret) == 0 {
		writeRegisteredProblem(w, r, http.StatusServiceUnavailable, "intent/security-unavailable", "Security Unavailable", problemcode.CodeSecurityUnavailable, "Decision token verification is not configured", nil)
		return verifiedDecisionToken{}, true
	}

	claims, err := auth.VerifyStrict(rawPlaybackDecisionToken, jwtSecret, "xg2g/v3/intents", "xg2g")
	if err != nil {
		code := auth.ClassifyError(err)
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "intent/unauthorized", "Unauthorized Intent", code, err.Error(), nil)
		return verifiedDecisionToken{}, true
	}
	if claims.TraceID != "" {
		verified.TraceID = claims.TraceID
	}

	normRef := normalize.ServiceRef(serviceRef)
	normTokenSub := normalize.ServiceRef(claims.Sub)
	if normRef != normTokenSub {
		writeRegisteredProblem(w, r, http.StatusForbidden, "intent/claim-mismatch", "Forbidden Action", problemcode.CodeClaimMismatch, "Token is not authorized for this service_ref", nil)
		return verifiedDecisionToken{}, true
	}

	reqMode := params["mode"]
	if raw := normalize.Token(reqMode); raw != "" && raw != claims.Mode {
		writeRegisteredProblem(w, r, http.StatusForbidden, "intent/claim-mismatch", "Forbidden Action", problemcode.CodeClaimMismatch, "Token is not authorized for this playback mode", nil)
		return verifiedDecisionToken{}, true
	}

	expectedHash := clientCapHash
	if expectedHash == "" {
		if rawCapHash := normalize.Token(params["capHash"]); rawCapHash != "" {
			expectedHash = rawCapHash
		} else if rawCapHash := normalize.Token(params["cap_hash"]); rawCapHash != "" {
			expectedHash = rawCapHash
		} else {
			genericMap := make(map[string]any, len(params))
			for k, v := range params {
				genericMap[k] = v
			}
			if cHash, err := normalize.MapHash(genericMap); err == nil {
				expectedHash = cHash
			}
		}
	}
	if claims.CapHash != "" && claims.CapHash != expectedHash {
		writeRegisteredProblem(w, r, http.StatusForbidden, "intent/claim-mismatch", "Forbidden Action", problemcode.CodeClaimMismatch, "Token is not authorized for these playback capabilities", nil)
		return verifiedDecisionToken{}, true
	}
	receipt, receiptDone := s.resolvePlannerReceipt(w, r, claims, serviceRef)
	if receiptDone {
		return verifiedDecisionToken{}, true
	}
	verified.Receipt = receipt

	if enforcePreflight(r.Context(), w, r, deps, serviceRef) {
		return verifiedDecisionToken{}, true
	}

	return verified, false
}

func derefStringIntentType(value *IntentRequestType) string {
	if value == nil {
		return ""
	}
	return string(*value)
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
		s.intentService = v3intents.NewService(
			&serverIntentDeps{s: s},
			v3intents.WithProfileResolver(s.profileResolver),
			v3intents.WithClientAV1Disabled(s.clientAV1Disabled),
			v3intents.WithIOSNativeHEVCHWMode(s.iosNativeHEVCHWMode),
		)
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
