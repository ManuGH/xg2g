// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net"
	"net/http"
	"strings"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	systemhttp "github.com/ManuGH/xg2g/internal/control/http/system"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/go-chi/chi/v5"
)

func (s *Server) newRouter() chi.Router {
	r := middleware.NewRouter(middleware.StackConfig{
		EnableCORS:           true,
		AllowedOrigins:       s.cfg.AllowedOrigins,
		CORSAllowCredentials: false, // PR3 requirement: hardcoded off

		EnableSecurityHeaders: true,
		CSP:                   middleware.DefaultCSP,
		TrustedProxies:        s.parsedTrustedProxies(),

		EnableMetrics:  true,
		TracingService: "xg2g-api",
		EnableLogging:  true,

		EnableRateLimit:    true,
		RateLimitEnabled:   s.cfg.RateLimitEnabled,
		RateLimitGlobalRPS: s.cfg.RateLimitGlobal,
		RateLimitBurst:     s.cfg.RateLimitBurst,
		RateLimitWhitelist: s.cfg.RateLimitWhitelist,
	})
	return r
}

func (s *Server) parsedTrustedProxies() []*net.IPNet {
	list := splitCSVNonEmpty(s.cfg.TrustedProxies)
	if len(list) == 0 {
		return nil
	}
	proxies, err := middleware.ParseCIDRs(list)
	if err != nil {
		log.L().Warn().Err(err).Msg("invalid trusted proxies configuration, ignoring value")
		return nil
	}
	return proxies
}

func splitCSVNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) registerPublicRoutes(r chi.Router) {
	r.Get("/healthz", systemhttp.NewHealthHandler(s.healthManager))
	r.Get("/readyz", systemhttp.NewReadyHandler(s.healthManager))

	r.Handle("/ui/*", http.StripPrefix("/ui", controlhttp.UIHandler(controlhttp.UIConfig{
		CSP: middleware.DefaultCSP,
	})))
	r.Get("/ui", redirectTo("/ui/", http.StatusMovedPermanently))
	r.Get("/", redirectTo("/ui/", http.StatusTemporaryRedirect))
}

func redirectTo(path string, code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, code)
	}
}

func (s *Server) scopedRouters(r chi.Router) (chi.Router, chi.Router, chi.Router, chi.Router, chi.Router) {
	rAuth := r.With(s.authMiddleware)
	rRead := rAuth.With(s.scopeMiddleware(v3.ScopeV3Read))
	rWrite := rAuth.With(s.scopeMiddleware(v3.ScopeV3Write))
	rAdmin := rAuth.With(s.scopeMiddleware(v3.ScopeV3Admin))
	rStatus := rAuth.With(s.scopeMiddleware(v3.ScopeV3Status))
	return rAuth, rRead, rWrite, rAdmin, rStatus
}

func (s *Server) registerOperatorRoutes(rAuth, rAdmin, rStatus chi.Router) {
	rAdmin.Post("/internal/system/config/reload", http.HandlerFunc(s.handleConfigReload))
	rStatus.Get(v3.V3BaseURL+"/status", controlhttp.NewStatusHandler(s.verificationStore).ServeHTTP)
	rAuth.Post("/internal/setup/validate", systemhttp.NewSetupValidateHandler())
}

func (s *Server) registerCanonicalV3Routes(r chi.Router) {
	// Register API v3 routes via canonical factory handler.
	// This keeps v3 middleware/routing decisions centralized in internal/control/http/v3/factory.go.
	v3Routes, err := v3.NewHandler(s.v3Handler, s.cfg)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to build v3 handler")
		r.Handle(v3.V3BaseURL+"/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}))
		return
	}
	// Register catch-all for canonical v3 routes.
	// More specific manual routes remain registered separately.
	r.Handle(v3.V3BaseURL+"/*", v3Routes)
}

func (s *Server) registerManualV3Routes(rRead, rWrite chi.Router) {
	rRead.Get(v3.V3BaseURL+"/vod/{recordingId}", func(w http.ResponseWriter, r *http.Request) {
		recordingID := chi.URLParam(r, "recordingId")
		s.v3Handler.GetRecordingPlaybackInfo(w, r, recordingID)
	})

	rRead.Head(v3.V3BaseURL+"/recordings/{recordingId}/stream.mp4", func(w http.ResponseWriter, r *http.Request) {
		recordingID := chi.URLParam(r, "recordingId")
		s.v3Handler.StreamRecordingDirect(w, r, recordingID)
	})

	rWrite.Put(v3.V3BaseURL+"/recordings/{recordingId}/resume", s.v3Handler.HandleRecordingResume)
	rWrite.Options(v3.V3BaseURL+"/recordings/{recordingId}/resume", s.v3Handler.HandleRecordingResumeOptions)
}

func (s *Server) registerClientPlaybackRoutes(rRead chi.Router) {
	// Supports DirectPlay decision logic without backend coupling.
	rRead.Post("/Items/{itemId}/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		itemID := chi.URLParam(r, "itemId")
		s.v3Handler.PostItemsPlaybackInfo(w, r, itemID)
	})
}

func (s *Server) newLANGuard() *middleware.LANGuard {
	lanGuard, err := middleware.NewLANGuard(middleware.LANGuardConfig{
		AllowedCIDRs:      append([]string(nil), s.cfg.Network.LAN.Allow.CIDRs...),
		TrustedProxyCIDRs: splitCSVNonEmpty(s.cfg.TrustedProxies),
	})
	if err == nil {
		return lanGuard
	}

	log.L().Warn().Err(err).Msg("invalid LAN guard CIDR configuration, falling back to defaults")
	fallback, fallbackErr := middleware.NewLANGuard(middleware.LANGuardConfig{})
	if fallbackErr != nil {
		// Should never happen (defaults are static), keep fail-closed semantics.
		log.L().Error().Err(fallbackErr).Msg("failed to initialize fallback LAN guard")
		return &middleware.LANGuard{}
	}
	return fallback
}
