// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"encoding/json"
	"net/http"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/log"
)

func (s *Server) SetConfigHolder(holder ConfigHolder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configHolder = holder
}

func (s *Server) ApplySnapshot(snap *config.Snapshot) {
	if snap == nil {
		return
	}

	// Structural Hardening: Enforce Immutability by deep-copying slice headers
	// This ensures that 'holder.Current()' mutation doesn't affect 's.cfg' and vice-versa.
	newCfg := snap.App

	// 1. Deep copy APITokenScopes
	if len(snap.App.APITokenScopes) > 0 {
		newScopes := make([]string, len(snap.App.APITokenScopes))
		copy(newScopes, snap.App.APITokenScopes)
		newCfg.APITokenScopes = newScopes
	}

	// 2. Deep copy APITokens and their scopes
	if len(snap.App.APITokens) > 0 {
		newTokens := make([]config.ScopedToken, len(snap.App.APITokens))
		for i, t := range snap.App.APITokens {
			newTokens[i] = t // copy struct
			if len(t.Scopes) > 0 {
				newTScopes := make([]string, len(t.Scopes))
				copy(newTScopes, t.Scopes)
				newTokens[i].Scopes = newTScopes
			}
		}
		newCfg.APITokens = newTokens
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = newCfg
	s.snap = *snap
	s.status.Version = snap.App.Version
}

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	holder := s.configHolder
	oldCfg := s.snap.App
	s.mu.RUnlock()

	if holder == nil {
		http.Error(w, "config reload not available", http.StatusNotImplemented)
		return
	}

	if err := holder.Reload(r.Context()); err != nil {
		logger := log.WithComponentFromContext(r.Context(), "config")
		logger.Warn().
			Err(err).
			Str("event", "config.reload_failed").
			Msg("config reload failed")
		http.Error(w, "config reload failed", http.StatusBadRequest)
		return
	}

	newSnap := holder.Current()
	if newSnap == nil {
		http.Error(w, "config reload failed", http.StatusInternalServerError)
		return
	}
	s.ApplySnapshot(newSnap)

	resp := struct {
		RestartRequired bool `json:"restart_required"`
	}{
		RestartRequired: reloadRequiresRestart(oldCfg, newSnap.App),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func reloadRequiresRestart(oldCfg, newCfg config.AppConfig) bool {
	if oldCfg.DataDir != newCfg.DataDir {
		return true
	}
	if oldCfg.APIListenAddr != newCfg.APIListenAddr {
		return true
	}
	if oldCfg.MetricsEnabled != newCfg.MetricsEnabled || oldCfg.MetricsAddr != newCfg.MetricsAddr {
		return true
	}
	if oldCfg.TLSCert != newCfg.TLSCert || oldCfg.TLSKey != newCfg.TLSKey || oldCfg.ForceHTTPS != newCfg.ForceHTTPS {
		return true
	}
	if oldCfg.OWIBase != newCfg.OWIBase || oldCfg.StreamPort != newCfg.StreamPort || oldCfg.UseWebIFStreams != newCfg.UseWebIFStreams {
		return true
	}
	// Security: Auth changes require restart to ensure consistent enforcement
	if oldCfg.APIToken != newCfg.APIToken {
		return true
	}
	if len(oldCfg.APITokens) != len(newCfg.APITokens) || len(oldCfg.APITokenScopes) != len(newCfg.APITokenScopes) {
		return true
	}
	// Deep compare APITokenScopes (slices)
	for i, v := range oldCfg.APITokenScopes {
		if v != newCfg.APITokenScopes[i] {
			return true
		}
	}
	// Deep compare APITokens (slice of structs)
	for i, oldToken := range oldCfg.APITokens {
		newToken := newCfg.APITokens[i]
		if oldToken.Token != newToken.Token {
			return true
		}
		if len(oldToken.Scopes) != len(newToken.Scopes) {
			return true
		}
		for j, s := range oldToken.Scopes {
			if s != newToken.Scopes[j] {
				return true
			}
		}
	}
	return false
}
