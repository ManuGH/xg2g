// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package api

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/http/deadline"
	systemhttp "github.com/ManuGH/xg2g/internal/control/http/system"
	v3 "github.com/ManuGH/xg2g/internal/control/http/v3"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/go-chi/chi/v5"
)

// piconFilenameRE matches safe picon filenames: hex/digit segments separated by underscores, ending in .png.
var piconFilenameRE = regexp.MustCompile(`^[0-9A-Fa-f_]+\.png$`)

var publicUIReservedPrefixes = []string{
	"/api",
	"/auth",
	"/stream",
	"/internal",
	"/logos",
	"/ui",
	"/Items",
}

func (s *Server) newRouter() chi.Router {
	// One-time honesty breadcrumb: the API rate limiter is window-based and ignores burst.
	// Warn only when the operator set a non-default value, so a deliberate (but inert) tuning
	// is surfaced instead of silently swallowed. Startup-only (newRouter runs once), not hot-path.
	if msg, warn := middleware.DeprecatedBurstWarning(s.cfg.RateLimitBurst); warn {
		log.L().Warn().Int("api_rate_limit_burst", s.cfg.RateLimitBurst).Msg(msg)
	}

	r := middleware.NewRouter(middleware.StackConfig{
		EnableCORS:           true,
		AllowedOrigins:       s.cfg.AllowedOrigins,
		CORSAllowCredentials: false, // PR3 requirement: hardcoded off

		EnableSecurityHeaders: true,
		CSP:                   middleware.DefaultCSP,
		TrustedProxies:        s.parsedTrustedProxies(),

		EnableCompression: true,

		EnableMetrics:  true,
		TracingService: "xg2g-api",
		EnableLogging:  true,

		EnableRateLimit:    true,
		RateLimitEnabled:   s.cfg.RateLimitEnabled,
		RateLimitGlobalRPS: s.cfg.RateLimitGlobal,
		RateLimitWhitelist: s.cfg.RateLimitWhitelist,

		MaxRequestBodyBytes: middleware.DefaultMaxRequestBodyBytes,
		DeadlineRuntimeMode: middleware.RuntimeEnforced,
		DeadlineTimeouts:    deadline.DefaultTimeouts(),
	})
	return r
}

// buildRouter constructs the canonical top-level chi.Router for a Server instance.
// This function is the single canonical factory used by production server startup (s.routes()) as well as governance inventory testing.
func (s *Server) buildRouter() chi.Router {
	variant, err := s.getUIConfigVariant()
	if err != nil {
		panic("resolve UI config variant: " + err.Error())
	}
	r, _, err := s.buildRouterWithBindings(variant)
	if err != nil {
		panic("build router with policy bindings: " + err.Error())
	}
	return r
}

func (s *Server) getUIConfigVariant() (ConfigVariant, error) {
	mode := controlhttp.DetermineUIMode(
		s.snap.Runtime.UIDevDir,
		s.snap.Runtime.UIDevProxyURL,
	)
	switch mode {
	case controlhttp.UIModeProdStatic:
		return ConfigVariantProdStatic, nil
	case controlhttp.UIModeDevDir:
		return ConfigVariantDevDir, nil
	case controlhttp.UIModeDevProxy:
		return ConfigVariantDevProxy, nil
	default:
		return "", fmt.Errorf("unsupported UI mode %q", mode)
	}
}

// buildRouterWithBindings is the single canonical production and governance
// router construction path.
func (s *Server) buildRouterWithBindings(variant ConfigVariant) (chi.Router, PolicyBindingSnapshot, error) {
	r := s.newRouter()
	r.Use(s.legacyAPIMiddleware)
	registry := deadline.NewPolicyBindingRegistry()

	adapterFor := func(router chi.Router, routerID, mountPrefix string) *policyRegistrarAdapter {
		return newPolicyRegistrarAdapter(policyRegistrarConfig{
			Router:      router,
			RouterID:    routerID,
			MountPrefix: mountPrefix,
			UIMode:      variant,
			Registry:    registry,
			RuntimeMode: middleware.RuntimeEnforced,
		})
	}

	rootAdapter := adapterFor(r, "outer", "")
	if err := s.registerPublicRoutesWithPolicies(rootAdapter, r); err != nil {
		return nil, PolicyBindingSnapshot{}, fmt.Errorf("register public routes: %w", err)
	}

	_, rRead, rWrite, rAdmin, rStatus := s.scopedRouters(r)
	if err := s.registerOperatorRoutesWithPolicies(
		adapterFor(rAdmin, "outer", ""),
		adapterFor(rStatus, "outer", ""),
	); err != nil {
		return nil, PolicyBindingSnapshot{}, fmt.Errorf("register operator routes: %w", err)
	}

	v3Sub := v3.NewRouteRouter()
	v3Adapter := adapterFor(v3Sub, "v3", v3.V3BaseURL)
	if err := v3.RegisterRoutes(v3Adapter, s.v3Handler); err != nil {
		return nil, PolicyBindingSnapshot{}, fmt.Errorf("register v3 routes: %w", err)
	}
	// v3Sub contains full /api/v3 patterns. A wildcard delegate preserves the
	// Phase 1 outer inventory's nine technical method entries.
	r.Handle(v3.V3BaseURL+"/*", v3Sub)

	readAdapter := adapterFor(rRead, "outer", "")
	writeAdapter := adapterFor(rWrite, "outer", "")
	if err := v3.RegisterCompatibilityRoutesWithRegistrars(readAdapter, writeAdapter, s.v3Handler); err != nil {
		return nil, PolicyBindingSnapshot{}, fmt.Errorf("register compatibility routes: %w", err)
	}
	if err := readAdapter.Register(http.MethodPost, "/Items/{itemId}/PlaybackInfo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.v3Handler.PostItemsPlaybackInfo(w, r, chi.URLParam(r, "itemId"))
	})); err != nil {
		return nil, PolicyBindingSnapshot{}, fmt.Errorf("register playback-info compatibility route: %w", err)
	}

	return r, registry.Snapshot(), nil
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

func (s *Server) registerPublicRoutesWithPolicies(adapter *policyRegistrarAdapter, r chi.Router) error {
	if err := adapter.Register(http.MethodGet, "/healthz", systemhttp.NewHealthHandler(s.healthManager)); err != nil {
		return err
	}
	if err := adapter.Register(http.MethodGet, "/readyz", systemhttp.NewReadyHandler(s.healthManager)); err != nil {
		return err
	}
	uiHandler := controlhttp.UIHandler(controlhttp.UIConfig{
		CSP:         middleware.DefaultCSP,
		DevProxyURL: s.snap.Runtime.UIDevProxyURL,
		DevDir:      s.snap.Runtime.UIDevDir,
	})
	uiWildcardHandler := http.StripPrefix("/ui", uiHandler)
	for _, method := range []string{
		http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodTrace,
		http.MethodConnect,
	} {
		if err := adapter.Register(method, "/ui/*", uiWildcardHandler); err != nil {
			return err
		}
	}
	if err := adapter.Register(http.MethodGet, "/ui", redirectTo("/ui/", http.StatusMovedPermanently)); err != nil {
		return err
	}
	if err := adapter.Register(http.MethodGet, "/index.html", redirectTo("/ui/", http.StatusTemporaryRedirect)); err != nil {
		return err
	}
	if err := adapter.Register(http.MethodGet, "/", redirectTo("/ui/", http.StatusTemporaryRedirect)); err != nil {
		return err
	}
	r.NotFound(s.publicNotFoundHandler(uiHandler))
	if err := adapter.Register(http.MethodGet, "/logos/{filename}", http.HandlerFunc(s.servePiconLogo)); err != nil {
		return err
	}
	return nil
}

// servePiconLogo securely serves a picon PNG from the data directory.
func (s *Server) servePiconLogo(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")

	// Strict validation: only hex/digit+underscore filenames ending in .png.
	if !piconFilenameRE.MatchString(filename) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	piconDir := filepath.Join(s.cfg.DataDir, "picons")
	fullPath := filepath.Join(piconDir, filename)

	// Defense-in-depth: ensure resolved path stays inside picons dir.
	absPath, err := filepath.Abs(fullPath)
	if err != nil || !strings.HasPrefix(absPath, filepath.Clean(piconDir)) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	f, err := os.Open(absPath) // #nosec G304
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.L().Warn().Err(closeErr).Str("path", absPath).Msg("failed to close picon file")
		}
	}()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeContent(w, r, filename, info.ModTime(), f)
}

func redirectTo(path string, code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path, code)
	}
}

func (s *Server) publicNotFoundHandler(uiHandler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if shouldServePublicUIFallback(r) {
			uiHandler.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	}
}

func shouldServePublicUIFallback(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}

	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return false
	}

	path := r.URL.Path
	if path == "" || path == "/" || strings.Contains(path, ".") {
		return false
	}

	switch path {
	case "/healthz", "/readyz", "/livez", "/metrics":
		return false
	}

	for _, prefix := range publicUIReservedPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return false
		}
	}

	return true
}

func (s *Server) scopedRouters(r chi.Router) (chi.Router, chi.Router, chi.Router, chi.Router, chi.Router) {
	rAuth := r.With(s.authMiddleware)
	rRead := rAuth.With(s.scopeMiddleware(v3.ScopeV3Read))
	rWrite := rAuth.With(s.scopeMiddleware(v3.ScopeV3Write))
	rAdmin := rAuth.With(s.scopeMiddleware(v3.ScopeV3Admin))
	rStatus := rAuth.With(s.scopeMiddleware(v3.ScopeV3Status))
	return rAuth, rRead, rWrite, rAdmin, rStatus
}

func (s *Server) registerOperatorRoutesWithPolicies(
	adminAdapter, statusAdapter *policyRegistrarAdapter,
) error {
	if err := adminAdapter.Register(http.MethodPost, "/internal/system/config/reload", http.HandlerFunc(s.handleConfigReload)); err != nil {
		return err
	}
	if err := statusAdapter.Register(http.MethodGet, v3.V3BaseURL+"/status", controlhttp.NewStatusHandler(s.verificationStore, s.cfg.FFmpeg.Bin)); err != nil {
		return err
	}
	return adminAdapter.Register(http.MethodPost, "/internal/setup/validate", systemhttp.NewSetupValidateHandler(s.GetConfig))
}
