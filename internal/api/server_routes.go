// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"net"
	"net/http"
	"strings"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/go-chi/chi/v5"
)

func (s *Server) routes() http.Handler {
	var trustedProxies []*net.IPNet
	if list := strings.Split(s.cfg.TrustedProxies, ","); len(list) > 0 {
		if tp, err := middleware.ParseCIDRs(list); err == nil {
			trustedProxies = tp
		}
	}

	r := middleware.NewRouter(middleware.StackConfig{
		EnableCORS:           true,
		AllowedOrigins:       s.cfg.AllowedOrigins,
		CORSAllowCredentials: false, // PR3 requirement: hardcoded off

		EnableSecurityHeaders: true,
		CSP:                   middleware.DefaultCSP,
		TrustedProxies:        trustedProxies,

		EnableMetrics:  true,
		TracingService: "xg2g-api",
		EnableLogging:  true,

		EnableRateLimit:    true,
		RateLimitEnabled:   s.cfg.RateLimitEnabled,
		RateLimitGlobalRPS: s.cfg.RateLimitGlobal,
		RateLimitBurst:     s.cfg.RateLimitBurst,
		RateLimitWhitelist: s.cfg.RateLimitWhitelist,
	})

	// 1. PUBLIC Endpoints (No Auth)
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)

	r.Handle("/ui/*", http.StripPrefix("/ui", controlhttp.UIHandler(controlhttp.UIConfig{
		CSP: middleware.DefaultCSP,
	})))

	r.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusTemporaryRedirect)
	})

	// 2. AUTHENTICATED Group (Fail-closed base)
	rAuth := r.With(s.authMiddleware)

	// 3. SCOPED Groups
	rRead := rAuth.With(s.scopeMiddleware(v3.ScopeV3Read))
	rWrite := rAuth.With(s.scopeMiddleware(v3.ScopeV3Write))
	rAdmin := rAuth.With(s.scopeMiddleware(v3.ScopeV3Admin))

	// 4. Admin Operations
	rAdmin.Post("/internal/system/config/reload", http.HandlerFunc(s.handleConfigReload))

	// 4.1 Status (Operator-Grade Contract)
	rStatus := rAuth.With(s.scopeMiddleware(v3.ScopeV3Status))
	rStatus.Get(v3.V3BaseURL+"/status", controlhttp.NewStatusHandler(s.verificationStore).ServeHTTP)

	// 5. Setup Validation
	rAuth.Post("/internal/setup/validate", http.HandlerFunc(s.handleSetupValidate))

	// 6. Register API v3 Routes via canonical factory handler.
	// This keeps v3 middleware/routing decisions centralized in internal/control/http/v3/factory.go.
	v3Routes, err := v3.NewHandler(s.v3Handler, s.cfg)
	if err != nil {
		log.L().Error().Err(err).Msg("failed to build v3 handler")
		r.Handle(v3.V3BaseURL+"/*", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}))
	} else {
		// Register catch-all for canonical v3 routes.
		// More specific manual routes below remain registered separately.
		r.Handle(v3.V3BaseURL+"/*", v3Routes)
	}

	// 7. Manual v3 Extensions (Strictly Scoped)
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

	// 8. Client Integration (Neutral Shape)
	// Supports DirectPlay decision logic without backend coupling
	rRead.Post("/Items/{itemId}/PlaybackInfo", func(w http.ResponseWriter, r *http.Request) {
		itemID := chi.URLParam(r, "itemId")
		s.v3Handler.PostItemsPlaybackInfo(w, r, itemID)
	})

	// 9. LAN Guard (Restrict discovery/legacy endpoints to private networks)
	// trusted proxies are comma-separated in config
	var proxies []string
	if s.cfg.TrustedProxies != "" {
		proxies = strings.Split(s.cfg.TrustedProxies, ",")
	}
	allowedCIDRs := append([]string(nil), s.cfg.Network.LAN.Allow.CIDRs...)
	lanGuard, _ := middleware.NewLANGuard(middleware.LANGuardConfig{
		AllowedCIDRs:      allowedCIDRs,
		TrustedProxyCIDRs: proxies,
	})

	s.registerLegacyRoutes(r, lanGuard)

	return r
}
