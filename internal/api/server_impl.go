// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"context" // This import is necessary for context.Context

	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

// Validation Request Model
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

// handleSetupValidate implements the validation endpoint
func (s *Server) handleSetupValidate(w http.ResponseWriter, r *http.Request) {
	var req setupValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.BaseURL == "" {
		http.Error(w, "Base URL is required", http.StatusBadRequest)
		return
	}

	// Smart Fix: Handle missing scheme (lazy user input)
	if !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
		req.BaseURL = "http://" + req.BaseURL
	}

	// Create ephemeral client
	client := openwebif.NewWithPort(req.BaseURL, 0, openwebif.Options{
		Timeout:  5 * time.Second, // Fast timeout for validation
		Username: req.Username,
		Password: req.Password,
	})

	// 1. Check Connectivity (Get About Info)
	about, err := client.About(r.Context())
	if err != nil {
		// Log detailed error but return generic message to UI (or detailed if safe)
		log.L().Warn().Err(err).Str("baseUrl", req.BaseURL).Msg("validation failed: connection error")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(setupValidateResponse{
			Valid:   false,
			Message: fmt.Sprintf("Connection failed: %s", err.Error()),
		})
		return
	}

	// 2. Fetch Bouquets (Metadata)
	bouquetsMap, err := client.Bouquets(r.Context())
	if err != nil {
		log.L().Warn().Err(err).Msg("validation warning: could not fetch bouquets")
	}

	bouquetsList := make([]string, 0, len(bouquetsMap))
	for name := range bouquetsMap {
		bouquetsList = append(bouquetsList, name)
	}
	sort.Strings(bouquetsList)

	// Success
	// Success
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(setupValidateResponse{
		Valid:    true,
		Message:  "Connection successful",
		Version:  about,
		Bouquets: bouquetsList,
	})
}

// StartRecordingCacheEvicter delegates to the v3 handler.
func (s *Server) StartRecordingCacheEvicter(ctx context.Context) {
	s.v3Handler.StartRecordingCacheEvicter(ctx)
}

func (s *Server) tokenPrincipal(token string) (*auth.Principal, bool) {
	return s.v3Handler.TokenPrincipal(token)
}

func (s *Server) scopeMiddleware(required ...v3.Scope) func(http.Handler) http.Handler {
	return s.v3Handler.ScopeMiddleware(required...)
}
