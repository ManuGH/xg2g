// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"errors"
	"net/http"
	"time"

	v3deviceauth "github.com/ManuGH/xg2g/internal/control/http/v3/deviceauth"
	connectivitydomain "github.com/ManuGH/xg2g/internal/domain/connectivity"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

const webBootstrapHeaderName = "X-XG2G-Web-Bootstrap"

type createDeviceSessionRequest struct {
	DeviceGrantID string `json:"deviceGrantId"`
	DeviceGrant   string `json:"deviceGrant"`
}

type createDeviceSessionResponse struct {
	DeviceID                    string                      `json:"deviceId"`
	RotatedDeviceGrantID        string                      `json:"rotatedDeviceGrantId,omitempty"`
	RotatedDeviceGrant          string                      `json:"rotatedDeviceGrant,omitempty"`
	RotatedDeviceGrantExpiresAt *string                     `json:"rotatedDeviceGrantExpiresAt,omitempty"`
	AccessSessionID             string                      `json:"accessSessionId"`
	AccessToken                 string                      `json:"accessToken"`
	AccessTokenExpiresAt        string                      `json:"accessTokenExpiresAt"`
	PolicyVersion               string                      `json:"policyVersion"`
	Scopes                      []string                    `json:"scopes"`
	Endpoints                   []publishedEndpointResponse `json:"endpoints"`
}

type createWebBootstrapRequest struct {
	TargetPath string `json:"targetPath"`
}

type createWebBootstrapResponse struct {
	BootstrapID    string `json:"bootstrapId"`
	BootstrapToken string `json:"bootstrapToken"`
	CompletePath   string `json:"completePath"`
	TargetPath     string `json:"targetPath"`
	ExpiresAt      string `json:"expiresAt"`
}

func (s *Server) CreateDeviceSession(w http.ResponseWriter, r *http.Request) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopePairing) {
		return
	}

	var req createDeviceSessionRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/device_session/invalid_input", "Invalid Device Session Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	result, err := s.deviceAuthProcessor().RefreshSession(r.Context(), v3deviceauth.RefreshSessionInput{
		DeviceGrantID: req.DeviceGrantID,
		DeviceGrant:   req.DeviceGrant,
	})
	if err != nil {
		writeDeviceSessionServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, createDeviceSessionResponse{
		DeviceID:                    result.DeviceID,
		RotatedDeviceGrantID:        result.RotatedDeviceGrantID,
		RotatedDeviceGrant:          result.RotatedDeviceGrant,
		RotatedDeviceGrantExpiresAt: formatOptionalTime(result.RotatedGrantExpiresAt),
		AccessSessionID:             result.AccessSessionID,
		AccessToken:                 result.AccessToken,
		AccessTokenExpiresAt:        result.AccessTokenExpiresAt.UTC().Format(http.TimeFormat),
		PolicyVersion:               result.PolicyVersion,
		Scopes:                      append([]string(nil), result.Scopes...),
		Endpoints:                   mapPublishedEndpointResponses(result.Endpoints),
	})
}

func (s *Server) CreateWebBootstrap(w http.ResponseWriter, r *http.Request) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopeWeb) {
		return
	}

	var req createWebBootstrapRequest
	if err := decodePairingBody(r, &req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/web_bootstrap/invalid_input", "Invalid Web Bootstrap Request", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	result, err := s.deviceAuthProcessor().StartWebBootstrap(r.Context(), v3deviceauth.StartWebBootstrapInput{
		SourceAccessToken: extractBearerToken(r),
		TargetPath:        req.TargetPath,
	})
	if err != nil {
		writeWebBootstrapServiceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, createWebBootstrapResponse{
		BootstrapID:    result.BootstrapID,
		BootstrapToken: result.BootstrapToken,
		CompletePath:   result.CompletePath,
		TargetPath:     result.TargetPath,
		ExpiresAt:      result.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

func (s *Server) CompleteWebBootstrap(w http.ResponseWriter, r *http.Request, bootstrapId string, params CompleteWebBootstrapParams) {
	if !s.enforceConnectivityScope(w, r, connectivitydomain.FindingScopeWeb) {
		return
	}

	result, err := s.deviceAuthProcessor().CompleteWebBootstrap(r.Context(), v3deviceauth.CompleteWebBootstrapInput{
		BootstrapID:    bootstrapId,
		BootstrapToken: params.XXG2GWebBootstrap,
	})
	if err != nil {
		writeWebBootstrapServiceError(w, r, err)
		return
	}

	ttl := time.Until(result.AccessTokenExpiresAt)
	if ttl <= 0 {
		writeProblem(w, r, http.StatusGone, "auth/web_bootstrap/expired", "Web Bootstrap Expired", "AUTH_WEB_BOOTSTRAP_EXPIRED", "The web bootstrap session expired before it could be completed.", nil)
		return
	}
	if _, err := s.issueCookieSession(w, r, result.AccessToken, ttl); err != nil {
		if errors.Is(err, ErrHTTPSRequired) {
			writeRegisteredProblem(w, r, http.StatusBadRequest, "auth/web_bootstrap/https_required", "HTTPS Required", problemcode.CodeHTTPSRequired, "Web bootstrap requires HTTPS or a trusted HTTPS proxy; plain HTTP is only accepted from loopback", nil)
			return
		}
		writeProblem(w, r, http.StatusInternalServerError, "auth/web_bootstrap/internal_error", "Web Bootstrap Internal Error", "AUTH_WEB_BOOTSTRAP_INTERNAL_ERROR", "The web bootstrap request failed unexpectedly.", nil)
		return
	}

	http.Redirect(w, r, result.TargetPath, http.StatusSeeOther)
}

func writeDeviceSessionServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var serviceErr *v3deviceauth.Error
	if !errors.As(err, &serviceErr) {
		writeProblem(w, r, http.StatusInternalServerError, "auth/device_session/internal_error", "Device Session Internal Error", "AUTH_DEVICE_SESSION_INTERNAL_ERROR", "The device session request failed unexpectedly.", nil)
		return
	}

	switch serviceErr.Kind {
	case v3deviceauth.ErrorInvalidInput:
		writeProblem(w, r, http.StatusBadRequest, "auth/device_session/invalid_input", "Invalid Device Session Request", "AUTH_DEVICE_SESSION_INVALID_INPUT", serviceErr.Message, nil)
	case v3deviceauth.ErrorUnauthorized:
		writeProblem(w, r, http.StatusUnauthorized, "auth/device_session/unauthorized", "Unauthorized", "AUTH_DEVICE_SESSION_UNAUTHORIZED", serviceErr.Message, nil)
	case v3deviceauth.ErrorNotFound:
		writeProblem(w, r, http.StatusNotFound, "auth/device_session/not_found", "Device Grant Not Found", "AUTH_DEVICE_SESSION_NOT_FOUND", serviceErr.Message, nil)
	case v3deviceauth.ErrorForbidden:
		writeProblem(w, r, http.StatusForbidden, "auth/device_session/forbidden", "Device Grant Forbidden", "AUTH_DEVICE_SESSION_FORBIDDEN", serviceErr.Message, nil)
	case v3deviceauth.ErrorConflict:
		writeProblem(w, r, http.StatusConflict, "auth/device_session/conflict", "Device Session Conflict", "AUTH_DEVICE_SESSION_CONFLICT", serviceErr.Message, nil)
	case v3deviceauth.ErrorExpired:
		writeProblem(w, r, http.StatusGone, "auth/device_session/expired", "Device Grant Expired", "AUTH_DEVICE_SESSION_EXPIRED", serviceErr.Message, nil)
	case v3deviceauth.ErrorRevoked:
		writeProblem(w, r, http.StatusGone, "auth/device_session/revoked", "Device Grant Revoked", "AUTH_DEVICE_SESSION_REVOKED", serviceErr.Message, nil)
	case v3deviceauth.ErrorStore:
		writeProblem(w, r, http.StatusServiceUnavailable, "auth/device_session/store_unavailable", "Device Session Store Unavailable", "AUTH_DEVICE_SESSION_STORE_UNAVAILABLE", serviceErr.Message, nil)
	default:
		writeProblem(w, r, http.StatusInternalServerError, "auth/device_session/internal_error", "Device Session Internal Error", "AUTH_DEVICE_SESSION_INTERNAL_ERROR", serviceErr.Message, nil)
	}
}

func writeWebBootstrapServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var serviceErr *v3deviceauth.Error
	if !errors.As(err, &serviceErr) {
		writeProblem(w, r, http.StatusInternalServerError, "auth/web_bootstrap/internal_error", "Web Bootstrap Internal Error", "AUTH_WEB_BOOTSTRAP_INTERNAL_ERROR", "The web bootstrap request failed unexpectedly.", nil)
		return
	}

	switch serviceErr.Kind {
	case v3deviceauth.ErrorInvalidInput:
		writeProblem(w, r, http.StatusBadRequest, "auth/web_bootstrap/invalid_input", "Invalid Web Bootstrap Request", "AUTH_WEB_BOOTSTRAP_INVALID_INPUT", serviceErr.Message, nil)
	case v3deviceauth.ErrorUnauthorized:
		writeProblem(w, r, http.StatusUnauthorized, "auth/web_bootstrap/unauthorized", "Unauthorized", "AUTH_WEB_BOOTSTRAP_UNAUTHORIZED", serviceErr.Message, nil)
	case v3deviceauth.ErrorNotFound:
		writeProblem(w, r, http.StatusNotFound, "auth/web_bootstrap/not_found", "Web Bootstrap Not Found", "AUTH_WEB_BOOTSTRAP_NOT_FOUND", serviceErr.Message, nil)
	case v3deviceauth.ErrorForbidden:
		writeProblem(w, r, http.StatusForbidden, "auth/web_bootstrap/forbidden", "Web Bootstrap Forbidden", "AUTH_WEB_BOOTSTRAP_FORBIDDEN", serviceErr.Message, nil)
	case v3deviceauth.ErrorConflict:
		writeProblem(w, r, http.StatusConflict, "auth/web_bootstrap/conflict", "Web Bootstrap Conflict", "AUTH_WEB_BOOTSTRAP_CONFLICT", serviceErr.Message, nil)
	case v3deviceauth.ErrorExpired:
		writeProblem(w, r, http.StatusGone, "auth/web_bootstrap/expired", "Web Bootstrap Expired", "AUTH_WEB_BOOTSTRAP_EXPIRED", serviceErr.Message, nil)
	case v3deviceauth.ErrorConsumed:
		writeProblem(w, r, http.StatusGone, "auth/web_bootstrap/consumed", "Web Bootstrap Already Used", "AUTH_WEB_BOOTSTRAP_CONSUMED", serviceErr.Message, nil)
	case v3deviceauth.ErrorRevoked:
		writeProblem(w, r, http.StatusGone, "auth/web_bootstrap/revoked", "Web Bootstrap Revoked", "AUTH_WEB_BOOTSTRAP_REVOKED", serviceErr.Message, nil)
	case v3deviceauth.ErrorStore:
		writeProblem(w, r, http.StatusServiceUnavailable, "auth/web_bootstrap/store_unavailable", "Web Bootstrap Store Unavailable", "AUTH_WEB_BOOTSTRAP_STORE_UNAVAILABLE", serviceErr.Message, nil)
	default:
		writeProblem(w, r, http.StatusInternalServerError, "auth/web_bootstrap/internal_error", "Web Bootstrap Internal Error", "AUTH_WEB_BOOTSTRAP_INTERNAL_ERROR", serviceErr.Message, nil)
	}
}
