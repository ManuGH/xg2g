// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
)

type setupValidateRequest struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupValidateResponse struct {
	Valid    bool                 `json:"valid"`
	Message  string               `json:"message"`
	Bouquets []string             `json:"bouquets,omitempty"`
	Version  *openwebif.AboutInfo `json:"version,omitempty"`
}

// NewSetupValidateHandler validates OpenWebIF connectivity and metadata.
func NewSetupValidateHandler(getConfig func() config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req setupValidateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.BaseURL == "" {
			http.Error(w, "Base URL is required", http.StatusBadRequest)
			return
		}

		// Normalize lazy user input to keep setup UX permissive.
		if !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
			req.BaseURL = "http://" + req.BaseURL
		}

		var cfg config.AppConfig
		if getConfig != nil {
			cfg = getConfig()
		}
		normalizedBaseURL, err := validateSetupBaseURL(r.Context(), req.BaseURL, cfg)
		if err != nil {
			log.L().Warn().
				Err(err).
				Str("base_url", safeURLForLog(req.BaseURL)).
				Msg("validation rejected: setup target not allowed by outbound policy")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(setupValidateResponse{
				Valid:   false,
				Message: "Connection target rejected by outbound policy",
			})
			return
		}
		req.BaseURL = normalizedBaseURL

		client := openwebif.NewWithPort(req.BaseURL, 0, openwebif.Options{
			Timeout:  5 * time.Second,
			Username: req.Username,
			Password: req.Password,
		})

		about, err := client.About(r.Context())
		if err != nil {
			log.L().Warn().Err(err).Str("base_url", safeURLForLog(req.BaseURL)).Msg("validation failed: connection error")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(setupValidateResponse{
				Valid:   false,
				Message: fmt.Sprintf("Connection failed: %s", err.Error()),
			})
			return
		}

		bouquetsMap, err := client.Bouquets(r.Context())
		if err != nil {
			log.L().Warn().Err(err).Msg("validation warning: could not fetch bouquets")
		}

		bouquetsList := make([]string, 0, len(bouquetsMap))
		for name := range bouquetsMap {
			bouquetsList = append(bouquetsList, name)
		}
		sort.Strings(bouquetsList)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(setupValidateResponse{
			Valid:    true,
			Message:  "Connection successful",
			Version:  about,
			Bouquets: bouquetsList,
		})
	}
}

func validateSetupBaseURL(ctx context.Context, rawBaseURL string, cfg config.AppConfig) (string, error) {
	normalized, err := platformnet.ValidateOutboundURL(ctx, rawBaseURL, outboundPolicyFromConfig(cfg))
	if err != nil {
		if errors.Is(err, platformnet.ErrOutboundDisabled) {
			return "", fmt.Errorf("outbound policy disabled: configure network.outbound allowlist for setup validation")
		}
		return "", err
	}
	return normalized, nil
}

func outboundPolicyFromConfig(cfg config.AppConfig) platformnet.OutboundPolicy {
	allow := cfg.Network.Outbound.Allow
	return platformnet.OutboundPolicy{
		Enabled: cfg.Network.Outbound.Enabled,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   append([]string(nil), allow.Hosts...),
			CIDRs:   append([]string(nil), allow.CIDRs...),
			Ports:   append([]int(nil), allow.Ports...),
			Schemes: append([]string(nil), allow.Schemes...),
		},
	}
}

func safeURLForLog(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "<invalid-url>"
	}
	if u.Scheme == "" {
		return u.Host
	}
	return u.Scheme + "://" + u.Host
}
