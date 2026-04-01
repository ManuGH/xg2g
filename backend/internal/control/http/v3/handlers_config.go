// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/read"
	"github.com/ManuGH/xg2g/internal/health"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	cfgvalidate "github.com/ManuGH/xg2g/internal/validate"
)

// Responsibility: Handles system configuration reading and updating.
// Non-goals: System status monitoring or hardware info (see system_info.go).

// GetSystemConfig implements ServerInterface
func (s *Server) GetSystemConfig(w http.ResponseWriter, r *http.Request) {
	deps := s.configModuleDeps()
	cfg := deps.cfg

	info := read.GetConfigInfo(cfg)
	monetization := buildMonetizationStatus(cfg.Monetization, auth.PrincipalFromContext(r.Context()))

	epgSource := EPGConfigSource(info.EPGSource)
	deliveryPolicy := StreamingConfigDeliveryPolicy(info.DeliveryPolicy)

	openWebIF := &OpenWebIFConfig{
		BaseUrl:    &info.Enigma2BaseURL,
		StreamPort: &info.Enigma2StreamPort,
	}
	if info.Enigma2Username != "" {
		openWebIF.Username = &info.Enigma2Username
	}

	resp := AppConfig{
		Version:   &info.Version,
		DataDir:   &info.DataDir,
		LogLevel:  &info.LogLevel,
		OpenWebIF: openWebIF,
		Bouquets:  &info.Bouquets,
		Epg: &EPGConfig{
			Days:    &info.EPGDays,
			Enabled: &info.EPGEnabled,
			Source:  &epgSource,
		},
		Picons: &PiconsConfig{
			BaseUrl: &info.PiconBase,
		},
		Streaming: &StreamingConfig{
			DeliveryPolicy: &deliveryPolicy,
		},
		Monetization: monetization,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// PutSystemConfig implements ServerInterface
func (s *Server) PutSystemConfig(w http.ResponseWriter, r *http.Request) {
	var req ConfigUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Request Format", problemcode.CodeInvalidInput, "The request body could not be decoded as JSON", nil)
		return
	}

	// 1. Serialization: dedicated config lock.
	s.configMu.Lock()
	defer s.configMu.Unlock()

	// 2. Clone: current baseline for modification and diffing.
	deps := s.configModuleDeps()
	current := deps.cfg
	next := config.Clone(current)

	if req.OpenWebIF != nil {
		if req.OpenWebIF.BaseUrl != nil {
			val := *req.OpenWebIF.BaseUrl
			if val != "" && !strings.HasPrefix(val, "http://") && !strings.HasPrefix(val, "https://") {
				val = "http://" + val
			}
			next.Enigma2.BaseURL = val
		}
		if req.OpenWebIF.Username != nil {
			next.Enigma2.Username = *req.OpenWebIF.Username
		}
		if req.OpenWebIF.Password != nil {
			next.Enigma2.Password = *req.OpenWebIF.Password
		}
		if req.OpenWebIF.StreamPort != nil {
			next.Enigma2.StreamPort = *req.OpenWebIF.StreamPort
		}
	}

	if req.Verification != nil {
		if req.Verification.Enabled != nil {
			next.Verification.Enabled = *req.Verification.Enabled
		}
		if req.Verification.Interval != nil {
			dur, err := time.ParseDuration(*req.Verification.Interval)
			if err != nil {
				writeRegisteredProblem(w, r, http.StatusBadRequest, "system/invalid_input", "Invalid Interval", problemcode.CodeInvalidInput, "Verification interval must be a valid duration string", nil)
				return
			}
			next.Verification.Interval = dur
		}
	}

	if req.Bouquets != nil {
		next.Bouquet = strings.Join(*req.Bouquets, ",")
	}

	if req.Epg != nil {
		if req.Epg.Enabled != nil {
			next.EPGEnabled = *req.Epg.Enabled
		}
		if req.Epg.Days != nil {
			next.EPGDays = *req.Epg.Days
		}
		if req.Epg.Source != nil {
			next.EPGSource = string(*req.Epg.Source)
		}
	}

	if req.Picons != nil {
		if req.Picons.BaseUrl != nil {
			next.PiconBase = *req.Picons.BaseUrl
		}
	}

	if req.LogLevel != nil {
		next.LogLevel = *req.LogLevel
	}

	// 3. Validate & Sanity Check
	if err := config.Validate(next); err != nil {
		respondConfigValidationError(w, r, err)
		return
	}
	if err := health.PerformStartupChecks(r.Context(), next); err != nil {
		respondConfigValidationError(w, r, err)
		return
	}

	// 4. Persistence
	if deps.configManager == nil {
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/save_failed", "Save Failed", problemcode.CodeSaveFailed, "Configuration manager is not initialized", nil)
		return
	}
	if err := deps.configManager.Save(&next); err != nil {
		log.L().Error().Err(err).Msg("failed to save configuration")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/save_failed", "Save Failed", problemcode.CodeSaveFailed, "Failed to save configuration change to disk", nil)
		return
	}

	// 5. Atomic Apply
	s.mu.Lock()
	s.cfg = next
	s.mu.Unlock()

	// 6. Side Effects (Hot Reload vs Restart)
	diff, err := config.Diff(current, next)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to diff configuration")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/diff_failed", "Diff Failed", problemcode.CodeDiffFailed, "Failed to compute configuration differences", nil)
		return
	}
	restartRequired := diff.RestartRequired

	if req.LogLevel != nil {
		principalID := "anonymous"
		var scopes []string
		if p := auth.PrincipalFromContext(r.Context()); p != nil {
			principalID = p.ID
			scopes = p.Scopes
		}
		if err := log.SetLevel(r.Context(), principalID, scopes, *req.LogLevel); err != nil {
			log.L().Error().Err(err).Msg("failed to hot-reload log level")
			// We continue here as the config IS already saved and applied to memory.
			// The log level failure is a secondary side-effect.
		}
	}

	respObj := struct {
		RestartRequired bool `json:"restartRequired"`
	}{
		RestartRequired: restartRequired,
	}

	status := http.StatusOK
	if restartRequired {
		status = http.StatusAccepted
	}
	writeJSON(w, status, respObj)

	if restartRequired {
		if rc := http.NewResponseController(w); rc != nil {
			_ = rc.Flush()
		}

		go func() {
			log.L().Info().Msg("configuration updated, triggering graceful shutdown")
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
			defer cancel()
			if deps.requestShutdown == nil {
				log.L().Error().Msg("graceful shutdown request failed: no shutdown handler registered")
				return
			}
			if err := deps.requestShutdown(ctx); err != nil {
				log.L().Error().Err(err).Msg("graceful shutdown request failed")
			}
		}()
	}
}

type configValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Value   any    `json:"value,omitempty"`
}

func respondConfigValidationError(w http.ResponseWriter, r *http.Request, err error) {
	var details []configValidationIssue

	var vErr cfgvalidate.ValidationError
	if errors.As(err, &vErr) {
		for _, item := range vErr.Errors() {
			details = append(details, configValidationIssue{
				Field:   item.Field,
				Message: item.Message,
				Value:   item.Value,
			})
		}
	} else {
		details = append(details, configValidationIssue{
			Field:   "preflight",
			Message: err.Error(),
		})
	}

	RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, details)
}

func buildMonetizationStatus(cfg config.MonetizationConfig, principal *auth.Principal) *MonetizationStatus {
	normalized := cfg.Normalized()
	if !normalized.Enabled && normalized.Enforcement == config.MonetizationEnforcementNone {
		return nil
	}

	enabled := normalized.Enabled
	model := normalized.Model
	productName := normalized.ProductName
	requiredScopes := append([]string(nil), normalized.RequiredScopes...)
	enforcement := normalized.Enforcement
	unlocked := !normalized.RequiresUnlock() || principalHasAllScopes(principal, requiredScopes)

	status := &MonetizationStatus{
		Enabled:        &enabled,
		Model:          &model,
		ProductName:    &productName,
		RequiredScopes: &requiredScopes,
		Enforcement:    &enforcement,
		Unlocked:       &unlocked,
	}
	if normalized.PurchaseURL != "" {
		purchaseURL := normalized.PurchaseURL
		status.PurchaseUrl = &purchaseURL
	}
	return status
}

func principalHasAllScopes(principal *auth.Principal, scopes []string) bool {
	if principal == nil {
		return false
	}
	normalizedCandidates := make(map[string]struct{}, len(principal.Scopes))
	for _, candidate := range principal.Scopes {
		value := strings.ToLower(strings.TrimSpace(candidate))
		if value == "*" {
			return true
		}
		normalizedCandidates[value] = struct{}{}
	}
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		normalizedScope := strings.ToLower(strings.TrimSpace(scope))
		if normalizedScope == "" {
			return false
		}
		if _, ok := normalizedCandidates[normalizedScope]; !ok {
			return false
		}
	}
	return true
}
