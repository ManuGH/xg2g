package v3

import (
	"errors"
	"fmt"
	"net/http"

	controlauth "github.com/ManuGH/xg2g/internal/control/auth"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/ManuGH/xg2g/internal/normalize"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func (s *Server) issuePlannerReceipt(playbackInfo v3recordings.PlaybackInfoResult, req v3recordings.PlaybackInfoRequest, schemaType string) (*v3intents.PlanningHandoff, error) {
	s.mu.RLock()
	enabled := s.plannerReceiptEnabled
	store := s.plannerReceiptStore
	s.mu.RUnlock()
	if !enabled || schemaType != "live" || playbackInfo.Decision == nil || playbackInfo.Decision.Mode == decision.ModeDeny {
		return nil, nil
	}
	if store == nil {
		return nil, fmt.Errorf("planner receipt store is not initialized")
	}
	if playbackInfo.PlannerEvidence == nil {
		return nil, fmt.Errorf("planner evidence is unavailable")
	}

	record, err := store.IssueEquivalent(
		*playbackInfo.PlannerEvidence,
		playbackInfo.Decision,
		req.PrincipalID,
		normalize.ServiceRef(playbackInfo.SourceRef),
		"live",
	)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Server) resolvePlannerReceipt(w http.ResponseWriter, r *http.Request, claims *v3auth.TokenClaims, serviceRef string) (*v3intents.PlanningHandoff, bool) {
	s.mu.RLock()
	enabled := s.plannerReceiptEnabled
	required := s.plannerReceiptRequired
	store := s.plannerReceiptStore
	s.mu.RUnlock()

	claimCount := plannerReceiptClaimCount(claims)
	if claimCount == 0 {
		if required {
			writeRegisteredProblem(w, r, http.StatusConflict, "intent/planner-receipt-missing", "Playback Info Refresh Required", problemcode.CodePlannerReceiptMissing, "The decision token has no planner receipt; refresh playback info before starting", nil)
			return nil, true
		}
		return nil, false
	}
	if claimCount != 5 {
		writeRegisteredProblem(w, r, http.StatusConflict, "intent/planner-receipt-invalid", "Playback Info Refresh Required", problemcode.CodePlannerReceiptInvalid, "The decision token contains an incomplete planner receipt; refresh playback info", nil)
		return nil, true
	}
	if !enabled || store == nil {
		writeRegisteredProblem(w, r, http.StatusConflict, "intent/planner-receipt-unavailable", "Playback Info Refresh Required", problemcode.CodePlannerReceiptInvalid, "The planner receipt is no longer available; refresh playback info", nil)
		return nil, true
	}

	principalID := ""
	if principal := controlauth.PrincipalFromContext(r.Context()); principal != nil {
		principalID = principal.ID
	}
	record, err := store.Resolve(v3intents.PlanningHandoffBinding{
		ReceiptID:      claims.ReceiptID,
		EvidenceHash:   claims.EvidenceHash,
		PlanHash:       claims.PlanHash,
		PlannerVersion: claims.PlannerVersion,
		PolicyVersion:  claims.PolicyVersion,
		PrincipalID:    principalID,
		ServiceRef:     normalize.ServiceRef(serviceRef),
		Scope:          "live",
	})
	if err == nil {
		return &record, false
	}

	status := http.StatusConflict
	problemType := "intent/planner-receipt-invalid"
	code := problemcode.CodePlannerReceiptInvalid
	detail := "The planner receipt is invalid; refresh playback info"
	switch {
	case errors.Is(err, v3intents.ErrPlanningHandoffExpired):
		problemType = "intent/planner-receipt-expired"
		code = problemcode.CodePlannerReceiptExpired
		detail = "The planner receipt expired; refresh playback info"
	case errors.Is(err, v3intents.ErrPlanningHandoffMissing):
		problemType = "intent/planner-receipt-stale"
		code = problemcode.CodePlannerReceiptExpired
		detail = "The planner receipt is unavailable after restart or eviction; refresh playback info"
	case errors.Is(err, v3intents.ErrPlanningBindingMismatch),
		errors.Is(err, v3intents.ErrPlanningHashMismatch),
		errors.Is(err, v3intents.ErrPlanningVersionMismatch):
		status = http.StatusForbidden
		problemType = "intent/planner-receipt-conflict"
		code = problemcode.CodePlannerReceiptConflict
		detail = "The planner receipt does not authorize this request; refresh playback info"
	}
	writeRegisteredProblem(w, r, status, problemType, "Playback Info Refresh Required", code, detail, nil)
	return nil, true
}

func (s *Server) consumePlannerReceipt(w http.ResponseWriter, r *http.Request, receiptID, sessionID string) (*v3intents.PlanningHandoff, bool) {
	s.mu.RLock()
	store := s.plannerReceiptStore
	s.mu.RUnlock()
	if store == nil {
		writeRegisteredProblem(w, r, http.StatusConflict, "intent/planner-receipt-unavailable", "Playback Info Refresh Required", problemcode.CodePlannerReceiptInvalid, "The planner receipt store is unavailable; refresh playback info", nil)
		return nil, true
	}
	record, err := store.Consume(receiptID, sessionID)
	if err != nil {
		code := problemcode.CodePlannerReceiptConflict
		detail := "The planner receipt was already consumed by another session; refresh playback info"
		if errors.Is(err, v3intents.ErrPlanningHandoffExpired) || errors.Is(err, v3intents.ErrPlanningHandoffMissing) {
			code = problemcode.CodePlannerReceiptExpired
			detail = "The planner receipt expired before the session started; refresh playback info"
		}
		writeRegisteredProblem(w, r, http.StatusConflict, "intent/planner-receipt-consume-conflict", "Playback Info Refresh Required", code, detail, nil)
		return nil, true
	}
	return &record, false
}

func plannerReceiptClaimCount(claims *v3auth.TokenClaims) int {
	if claims == nil {
		return 0
	}
	count := 0
	for _, value := range []string{claims.ReceiptID, claims.PlanHash, claims.EvidenceHash, claims.PlannerVersion, claims.PolicyVersion} {
		if value != "" {
			count++
		}
	}
	return count
}
