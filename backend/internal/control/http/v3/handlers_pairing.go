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
	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

func (s *Server) StartPairing(w http.ResponseWriter, r *http.Request) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req StartPairingRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	// deviceType is an enum in the contract but only a string on the wire, so
	// an unknown value used to be stored verbatim — the Android client shipped
	// "tv" for a while and nothing objected. Reject it at the edge instead.
	deviceType := DeviceAuthDeviceTypeUnknown
	if req.DeviceType != nil {
		deviceType = *req.DeviceType
		if !deviceType.Valid() {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "deviceType is not a known device type", nil)
			return
		}
	}

	result, err := s.pairingProcessor().Start(r.Context(), v3pairing.StartInput{
		DeviceName:             stringValue(req.DeviceName),
		DeviceType:             v3pairingDeviceType(string(deviceType)),
		RequestedPolicyProfile: stringValue(req.RequestedPolicyProfile),
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, StartPairingResponse{
		PairingId:     result.PairingID,
		PairingSecret: result.PairingSecret,
		UserCode:      result.UserCode,
		QrPayload:     result.QRPayload,
		ExpiresAt:     result.ExpiresAt.UTC(),
	})
}

func (s *Server) GetPairingStatus(w http.ResponseWriter, r *http.Request, pairingId string) {
	var req PairingSecretRequest
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

	writeJSON(w, http.StatusOK, PairingStatusResponse{
		PairingId:              result.PairingID,
		Status:                 PairingStatus(result.Status),
		UserCode:               result.UserCode,
		DeviceName:             result.DeviceName,
		DeviceType:             DeviceAuthDeviceType(result.DeviceType),
		RequestedPolicyProfile: optionalStringPtr(result.RequestedPolicyProfile),
		ApprovedPolicyProfile:  optionalStringPtr(result.ApprovedPolicyProfile),
		ExpiresAt:              result.ExpiresAt.UTC(),
		ApprovedAt:             utcOrNil(result.ApprovedAt),
		ConsumedAt:             utcOrNil(result.ConsumedAt),
	})
}

func (s *Server) ApprovePairing(w http.ResponseWriter, r *http.Request, pairingId string) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req ApprovePairingRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	principal := ctrlauth.PrincipalFromContext(r.Context())
	if principal == nil {
		writeRegisteredProblem(w, r, http.StatusUnauthorized, "auth/unauthorized", "Unauthorized", problemcode.CodeUnauthorized, "Authentication required", nil)
		return
	}

	ownerID := stringValue(req.OwnerId)
	if ownerID == "" {
		ownerID = principal.ID
	}

	result, err := s.pairingProcessor().Approve(r.Context(), v3pairing.ApproveInput{
		PairingID:             pairingId,
		OwnerID:               ownerID,
		ApprovedPolicyProfile: stringValue(req.ApprovedPolicyProfile),
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, ApprovePairingResponse{
		PairingId:             result.PairingID,
		Status:                PairingStatus(result.Status),
		OwnerId:               result.OwnerID,
		ApprovedPolicyProfile: optionalStringPtr(result.ApprovedPolicyProfile),
		ApprovedAt:            utcOrNil(result.ApprovedAt),
		ExpiresAt:             result.ExpiresAt.UTC(),
	})
}

func (s *Server) ExchangePairing(w http.ResponseWriter, r *http.Request, pairingId string) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req PairingSecretRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "pairing/invalid_input", "Invalid Pairing Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	result, err := s.pairingProcessor().Exchange(r.Context(), v3pairing.ExchangeInput{
		PairingID:     pairingId,
		PairingSecret: req.PairingSecret,
		DeviceJWK:     domainDeviceJWK(req.DeviceJwk),
	})
	if err != nil {
		writePairingServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, ExchangePairingResponse{
		PairingId:     result.PairingID,
		DeviceId:      result.DeviceID,
		TokenType:     result.TokenType,
		AccessToken:   result.AccessToken,
		ExpiresIn:     contractInt32(result.ExpiresIn),
		RefreshToken:  result.RefreshToken,
		Scope:         result.Scope,
		PolicyVersion: result.PolicyVersion,
		Endpoints:     mapPublishedEndpointContracts(result.Endpoints),
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

// domainDeviceJWK converts the wire key into the identity domain's key.
//
// An explicit boundary rather than a shared struct: the contract type is
// regenerated from api/openapi.yaml, and the domain type must not silently
// inherit whatever the contract does next.
func domainDeviceJWK(jwk ECPublicKeyJWK) identity.JWKECPublicKey {
	return identity.JWKECPublicKey{
		Kty: string(jwk.Kty),
		Crv: string(jwk.Crv),
		X:   jwk.X,
		Y:   jwk.Y,
	}
}

// stringValue reads an optional contract field. Absent and empty mean the same
// thing to every caller here, which is why the pointer stops at this boundary.
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// utcOrNil normalises an optional timestamp without inventing one.
//
// The pairing responses used to carry pre-formatted strings, which is how they
// came to be HTTP header dates in a JSON body. The generated types carry
// time.Time and encoding/json renders RFC 3339, so the format is no longer a
// decision anybody can get wrong — only the zone still is.
func utcOrNil(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func v3pairingDeviceType(value string) deviceauthmodel.DeviceType {
	return deviceauthmodel.DeviceType(value)
}
