// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	ctrlauth "github.com/ManuGH/xg2g/internal/control/auth"
	v3pairing "github.com/ManuGH/xg2g/internal/control/http/v3/pairing"
	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

type startPairingRequest struct {
	DeviceName             string `json:"deviceName"`
	DeviceType             string `json:"deviceType"`
	RequestedPolicyProfile string `json:"requestedPolicyProfile"`
}

type approvePairingRequest struct {
	OwnerID               string `json:"ownerId"`
	ApprovedPolicyProfile string `json:"approvedPolicyProfile"`
}

type pairingSecretRequest struct {
	PairingSecret string `json:"pairingSecret"`
}

type startPairingResponse struct {
	PairingID     string `json:"pairingId"`
	PairingSecret string `json:"pairingSecret"`
	UserCode      string `json:"userCode"`
	QRPayload     string `json:"qrPayload"`
	ExpiresAt     string `json:"expiresAt"`
}

type pairingStatusResponse struct {
	PairingID              string  `json:"pairingId"`
	Status                 string  `json:"status"`
	UserCode               string  `json:"userCode"`
	DeviceName             string  `json:"deviceName"`
	DeviceType             string  `json:"deviceType"`
	RequestedPolicyProfile string  `json:"requestedPolicyProfile,omitempty"`
	ApprovedPolicyProfile  string  `json:"approvedPolicyProfile,omitempty"`
	ExpiresAt              string  `json:"expiresAt"`
	ApprovedAt             *string `json:"approvedAt,omitempty"`
	ConsumedAt             *string `json:"consumedAt,omitempty"`
}

type approvePairingResponse struct {
	PairingID             string  `json:"pairingId"`
	Status                string  `json:"status"`
	OwnerID               string  `json:"ownerId"`
	ApprovedPolicyProfile string  `json:"approvedPolicyProfile,omitempty"`
	ApprovedAt            *string `json:"approvedAt,omitempty"`
	ExpiresAt             string  `json:"expiresAt"`
}

type exchangePairingResponse struct {
	PairingID            string                      `json:"pairingId"`
	DeviceID             string                      `json:"deviceId"`
	DeviceGrantID        string                      `json:"deviceGrantId"`
	DeviceGrant          string                      `json:"deviceGrant"`
	DeviceGrantExpiresAt string                      `json:"deviceGrantExpiresAt"`
	AccessSessionID      string                      `json:"accessSessionId"`
	AccessToken          string                      `json:"accessToken"`
	AccessTokenExpiresAt string                      `json:"accessTokenExpiresAt"`
	PolicyVersion        string                      `json:"policyVersion"`
	Scopes               []string                    `json:"scopes"`
	Endpoints            []publishedEndpointResponse `json:"endpoints"`
}

func (s *Server) StartPairing(w http.ResponseWriter, r *http.Request) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req startPairingRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	result, err := s.pairingProcessor().Start(r.Context(), v3pairing.StartInput{
		DeviceName:             req.DeviceName,
		DeviceType:             v3pairingDeviceType(req.DeviceType),
		RequestedPolicyProfile: req.RequestedPolicyProfile,
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, startPairingResponse{
		PairingID:     result.PairingID,
		PairingSecret: result.PairingSecret,
		UserCode:      result.UserCode,
		QRPayload:     result.QRPayload,
		ExpiresAt:     result.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

func (s *Server) GetPairingStatus(w http.ResponseWriter, r *http.Request, pairingId string) {
	var req pairingSecretRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	result, err := s.pairingProcessor().Status(r.Context(), v3pairing.StatusInput{
		PairingID:     pairingId,
		PairingSecret: req.PairingSecret,
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, pairingStatusResponse{
		PairingID:              result.PairingID,
		Status:                 string(result.Status),
		UserCode:               result.UserCode,
		DeviceName:             result.DeviceName,
		DeviceType:             string(result.DeviceType),
		RequestedPolicyProfile: result.RequestedPolicyProfile,
		ApprovedPolicyProfile:  result.ApprovedPolicyProfile,
		ExpiresAt:              result.ExpiresAt.UTC().Format(http.TimeFormat),
		ApprovedAt:             formatOptionalTime(result.ApprovedAt),
		ConsumedAt:             formatOptionalTime(result.ConsumedAt),
	})
}

func (s *Server) ApprovePairing(w http.ResponseWriter, r *http.Request, pairingId string) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req approvePairingRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	principal := ctrlauth.PrincipalFromContext(r.Context())
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	ownerID := req.OwnerID
	if ownerID == "" {
		ownerID = principal.ID
	}

	result, err := s.pairingProcessor().Approve(r.Context(), v3pairing.ApproveInput{
		PairingID:             pairingId,
		OwnerID:               ownerID,
		ApprovedPolicyProfile: req.ApprovedPolicyProfile,
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, approvePairingResponse{
		PairingID:             result.PairingID,
		Status:                string(result.Status),
		OwnerID:               result.OwnerID,
		ApprovedPolicyProfile: result.ApprovedPolicyProfile,
		ApprovedAt:            formatOptionalTime(result.ApprovedAt),
		ExpiresAt:             result.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

func (s *Server) ExchangePairing(w http.ResponseWriter, r *http.Request, pairingId string) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req pairingSecretRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	result, err := s.pairingProcessor().Exchange(r.Context(), v3pairing.ExchangeInput{
		PairingID:     pairingId,
		PairingSecret: req.PairingSecret,
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, exchangePairingResponse{
		PairingID:            result.PairingID,
		DeviceID:             result.DeviceID,
		DeviceGrantID:        result.DeviceGrantID,
		DeviceGrant:          result.DeviceGrant,
		DeviceGrantExpiresAt: result.DeviceGrantExpiresAt.UTC().Format(http.TimeFormat),
		AccessSessionID:      result.AccessSessionID,
		AccessToken:          result.AccessToken,
		AccessTokenExpiresAt: result.AccessTokenExpiresAt.UTC().Format(http.TimeFormat),
		PolicyVersion:        result.PolicyVersion,
		Scopes:               append([]string(nil), result.Scopes...),
		Endpoints:            mapPublishedEndpointResponses(result.Endpoints),
	})
}

func writePairingServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var serviceErr *v3pairing.Error
	if !errors.As(err, &serviceErr) {
		writeProblem(w, r, http.StatusInternalServerError, "pairing/internal_error", "Pairing Internal Error", "PAIRING_INTERNAL_ERROR", "The pairing request failed unexpectedly.", nil)
		return
	}

	switch serviceErr.Kind {
	case v3pairing.ErrorInvalidInput:
		writeProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", "PAIRING_INVALID_INPUT", serviceErr.Message, nil)
	case v3pairing.ErrorNotFound:
		writeProblem(w, r, http.StatusNotFound, "pairing/not_found", "Pairing Not Found", "PAIRING_NOT_FOUND", serviceErr.Message, nil)
	case v3pairing.ErrorConflict:
		writeProblem(w, r, http.StatusConflict, "pairing/conflict", "Pairing Conflict", "PAIRING_CONFLICT", serviceErr.Message, nil)
	case v3pairing.ErrorForbidden:
		writeProblem(w, r, http.StatusForbidden, "pairing/secret_mismatch", "Pairing Secret Mismatch", "PAIRING_SECRET_MISMATCH", serviceErr.Message, nil)
	case v3pairing.ErrorPending:
		writeProblem(w, r, http.StatusConflict, "pairing/pending", "Pairing Pending Approval", "PAIRING_PENDING", serviceErr.Message, nil)
	case v3pairing.ErrorExpired:
		writeProblem(w, r, http.StatusGone, "pairing/expired", "Pairing Expired", "PAIRING_EXPIRED", serviceErr.Message, nil)
	case v3pairing.ErrorConsumed:
		writeProblem(w, r, http.StatusGone, "pairing/consumed", "Pairing Already Exchanged", "PAIRING_CONSUMED", serviceErr.Message, nil)
	case v3pairing.ErrorRevoked:
		writeProblem(w, r, http.StatusGone, "pairing/revoked", "Pairing Revoked", "PAIRING_REVOKED", serviceErr.Message, nil)
	case v3pairing.ErrorStore:
		writeProblem(w, r, http.StatusServiceUnavailable, "pairing/store_unavailable", "Pairing Store Unavailable", "PAIRING_STORE_UNAVAILABLE", serviceErr.Message, nil)
	default:
		writeProblem(w, r, http.StatusInternalServerError, "pairing/internal_error", "Pairing Internal Error", "PAIRING_INTERNAL_ERROR", serviceErr.Message, nil)
	}
}

func decodePairingBody(r *http.Request, out any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(out)
}

func formatOptionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(http.TimeFormat)
	return &formatted
}

func v3pairingDeviceType(value string) deviceauthmodel.DeviceType {
	return deviceauthmodel.DeviceType(value)
}
