package v3

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/receipts"
)

// PostSystemEntitlementReceipt implements ServerInterface.
func (s *Server) PostSystemEntitlementReceipt(w http.ResponseWriter, r *http.Request) {
	principal := auth.PrincipalFromContext(r.Context())
	if principal == nil {
		RespondError(w, r, http.StatusUnauthorized, ErrUnauthorized)
		return
	}

	var req PostSystemEntitlementReceiptJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, "receipt request body must be valid JSON")
		return
	}

	targetPrincipalID, _, err := s.resolveEntitlementTarget(principal, req.PrincipalId)
	if err != nil {
		RespondError(w, r, http.StatusForbidden, ErrForbidden, err.Error())
		return
	}

	receiptService := s.receiptServiceSnapshot()
	result, err := receiptService.VerifyAndApply(r.Context(), receipts.ApplyRequest{
		PrincipalID:   targetPrincipalID,
		Provider:      req.Provider,
		ProductID:     req.ProductId,
		PurchaseToken: req.PurchaseToken,
		UserID:        derefString(req.UserId),
	})
	if err != nil {
		statusCode, apiErr, detail := receiptErrorResponse(err)
		RespondError(w, r, statusCode, apiErr, detail)
		return
	}

	status, err := s.buildEntitlementStatus(r.Context(), targetPrincipalID, configuredPrincipalScopes(s.GetConfig(), targetPrincipalID))
	if err != nil {
		log.FromContext(r.Context()).Error().Err(err).Str("principal_id", targetPrincipalID).Msg("failed to build entitlement status after receipt verification")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to resolve entitlement status")
		return
	}

	resp := EntitlementReceiptResponse{
		PrincipalId:       &targetPrincipalID,
		Provider:          result.Verification.Provider,
		ProductId:         result.Verification.ProductID,
		Source:            &result.Verification.Source,
		PurchaseState:     string(result.Verification.State),
		Action:            string(result.Action),
		MappedScopes:      result.MappedScopes,
		EntitlementStatus: *status,
	}
	if result.Verification.OrderID != "" {
		resp.OrderId = &result.Verification.OrderID
	}
	if result.Verification.PurchaseTime != nil {
		purchaseTime := result.Verification.PurchaseTime.UTC()
		resp.PurchaseTime = &purchaseTime
	}
	resp.TestPurchase = &result.Verification.TestPurchase

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) receiptServiceSnapshot() *receipts.Service {
	s.mu.RLock()
	service := s.receiptService
	s.mu.RUnlock()
	return service
}

func receiptErrorResponse(err error) (int, *APIError, string) {
	var receiptErr *receipts.Error
	if errors.As(err, &receiptErr) {
		switch receiptErr.Kind {
		case receipts.ErrorKindInvalidInput:
			return http.StatusBadRequest, ErrInvalidInput, receiptErr.Error()
		case receipts.ErrorKindUnavailable:
			return http.StatusServiceUnavailable, ErrServiceUnavailable, receiptErr.Error()
		case receipts.ErrorKindUpstream:
			return http.StatusBadGateway, ErrUpstreamUnavailable, receiptErr.Error()
		default:
			return http.StatusBadGateway, ErrUpstreamUnavailable, receiptErr.Error()
		}
	}
	return http.StatusBadGateway, ErrUpstreamUnavailable, err.Error()
}
