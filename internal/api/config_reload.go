// SPDX-License-Identifier: MIT

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

func (s *Server) ApplyConfig(cfg config.AppConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
	s.snap = config.BuildSnapshot(cfg)
	s.status.Version = cfg.Version
}

func (s *Server) handleConfigReload(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	holder := s.configHolder
	oldCfg := s.cfg
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

	newCfg := holder.Get()
	s.ApplyConfig(newCfg)

	resp := struct {
		RestartRequired bool `json:"restart_required"`
	}{
		RestartRequired: reloadRequiresRestart(oldCfg, newCfg),
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
	return false
}
