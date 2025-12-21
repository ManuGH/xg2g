// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
	"golang.org/x/sync/singleflight"

	apimw "github.com/ManuGH/xg2g/internal/api/middleware"
	"github.com/ManuGH/xg2g/internal/auth"
	"github.com/ManuGH/xg2g/internal/config"
	coreopenwebif "github.com/ManuGH/xg2g/internal/core/openwebif"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/rs/zerolog"
)

// Server represents a reverse proxy server for Enigma2 streams.
type Server struct {
	addr         string
	targetURL    *url.URL // Fallback target URL (optional)
	proxy        *httputil.ReverseProxy
	httpServer   *http.Server
	logger       zerolog.Logger
	transcoder   *Transcoder // Optional audio transcoder
	receiverHost string      // Receiver host for fallback
	hlsManager   *HLSManager // HLS streaming manager for iOS
	tlsCert      string
	tlsKey       string
	dataDir      string // For reading playlist.m3u
	playlistPath string // Path to M3U playlist
	// channelMap stores StreamID -> StreamURL mappings.
	channelMap         map[string]string
	channelMu          sync.RWMutex
	listenPort         string
	localHosts         map[string]struct{}
	recordingRoots     map[string]string
	allowedAuthorities map[string]struct{} // Canonical host:port allowlist

	streamLimiter     *semaphore.Weighted
	streamLimit       int64
	transcodeFailOpen bool

	idleTimeout   time.Duration
	registry      *Registry // Replaces activeStreams sync.Map
	monitorCancel context.CancelFunc
	mu            sync.Mutex

	apiToken      string
	authAnonymous bool

	readyChecker   ReadyChecker
	sfg            singleflight.Group
	preflightCache sync.Map // Map[string]preflightCacheEntry
}

type preflightCacheEntry struct {
	status    int
	timestamp time.Time
}

var (
	ErrReadyTimeout   = errors.New("readiness check timed out")
	ErrInvariant      = errors.New("invariant violation")
	ErrNotReady       = errors.New("stream not ready")
	ErrStreamNotFound = errors.New("stream not found")
)

// Config holds the configuration for the proxy server.
type Config struct {
	ListenAddr     string
	TargetURL      string
	ReceiverHost   string
	Logger         zerolog.Logger
	TLSCert        string
	TLSKey         string
	DataDir        string
	PlaylistPath   string
	RecordingRoots map[string]string
	Runtime        config.RuntimeSnapshot
	APIToken       string
	AuthAnonymous  bool
	AllowedOrigins []string
}

// New creates a new proxy server.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		return nil, fmt.Errorf("listen address is required")
	}

	if cfg.TargetURL == "" && cfg.ReceiverHost == "" {
		return nil, fmt.Errorf("either TargetURL or ReceiverHost is required")
	}

	// Security Gate 1: Fail-Closed Auth
	// If anonymous auth is disabled, we MUST have a valid, non-empty token configured.
	if !cfg.AuthAnonymous && strings.TrimSpace(cfg.APIToken) == "" {
		return nil, fmt.Errorf("proxy authentication token is unset or empty, and anonymous access is disabled (refusing to start open proxy)")
	}

	// Ensure RecordingRoots has a default if empty to prevent surprising denials
	recordingRoots := cfg.RecordingRoots
	if len(recordingRoots) == 0 {
		recordingRoots = map[string]string{"hdd": "/media/hdd/movie"}
	}

	s := &Server{
		addr:           cfg.ListenAddr,
		logger:         cfg.Logger,
		receiverHost:   cfg.ReceiverHost,
		tlsCert:        cfg.TLSCert,
		tlsKey:         cfg.TLSKey,
		hlsManager:     nil, // init later
		dataDir:        cfg.DataDir,
		playlistPath:   cfg.PlaylistPath,
		recordingRoots: recordingRoots,
		channelMap:     make(map[string]string),
		registry:       NewRegistry(),
		localHosts:     make(map[string]struct{}),
		apiToken:       cfg.APIToken,
		authAnonymous:  cfg.AuthAnonymous,
	}

	listenHost, listenPort := splitListenAddr(cfg.ListenAddr)
	s.listenPort = listenPort
	s.localHosts = collectLocalHosts(listenHost)

	if err := s.loadM3U(); err != nil {
		cfg.Logger.Warn().Err(err).Msg("failed to load initial playlist (will retry on lookup)")
	}

	rt := cfg.Runtime

	if n := rt.StreamProxy.MaxConcurrentStreams; n > 0 {
		s.streamLimit = n
		s.streamLimiter = semaphore.NewWeighted(n)
	}

	s.transcodeFailOpen = rt.StreamProxy.TranscodeFailOpen

	if rt.StreamProxy.IdleTimeout > 0 {
		s.idleTimeout = rt.StreamProxy.IdleTimeout
	}

	// Security Gate 2: SSRF / Open Proxy Hardening
	// Pre-calculate allowed authorities (host:port) to prevent pivoting.
	s.allowedAuthorities = make(map[string]struct{})

	if cfg.TargetURL != "" {
		target, err := url.Parse(cfg.TargetURL)
		if err != nil {
			return nil, fmt.Errorf("parse target URL %q: %w", cfg.TargetURL, err)
		}
		// Enforce explicit port
		if target.Port() == "" {
			return nil, fmt.Errorf("configured TargetURL %q must include an explicit port (e.g. :80 or :443)", cfg.TargetURL)
		}

		auth := canonicalizeAuthority(target.Host)
		s.allowedAuthorities[auth] = struct{}{}

		s.targetURL = target

		s.proxy = httputil.NewSingleHostReverseProxy(target)
		s.proxy.ErrorLog = nil

		originalDirector := s.proxy.Director
		s.proxy.Director = func(req *http.Request) {
			originalDirector(req)
			req.Host = target.Host
		}
	} else if cfg.ReceiverHost != "" {
		// ReceiverHost legacy mode: implies port 8001
		// Validate that ReceiverHost is host-only (no port); port is implied by legacy behavior.
		if _, _, err := net.SplitHostPort(cfg.ReceiverHost); err == nil {
			return nil, fmt.Errorf("ReceiverHost %q must not include a port; configure TargetURL instead", cfg.ReceiverHost)
		}

		// Determine the authority we actually fallback to: ReceiverHost:8001
		receiverAuth := net.JoinHostPort(cfg.ReceiverHost, "8001")
		// Canonicalize
		s.allowedAuthorities[canonicalizeAuthority(receiverAuth)] = struct{}{}

		s.proxy = &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				targetURL, _ := s.resolveTargetURL(req.Context(), req.URL.Path, req.URL.RawQuery)
				target, _ := url.Parse(targetURL)
				if target != nil {
					req.URL.Scheme = target.Scheme
					req.URL.Host = target.Host
					req.URL.Path = target.Path
					req.Host = target.Host
				}
			},
			ErrorLog: nil,
		}
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	s.proxy.Transport = transport

	if rt.Transcoder.Enabled {
		transcoderCfg := buildTranscoderConfigFromRuntime(rt.Transcoder)
		if transcoderCfg.Enabled || transcoderCfg.H264RepairEnabled || transcoderCfg.GPUEnabled || transcoderCfg.VideoTranscode {
			s.transcoder = NewTranscoder(transcoderCfg, cfg.Logger.With().Str("component", "transcoder").Logger())
		}
	}

	hlsCfg := HLSManagerConfig{
		OutputDir:      rt.HLS.OutputDir,
		Generic:        rt.HLS.Generic,
		Safari:         rt.HLS.Safari,
		LLHLS:          rt.HLS.LLHLS,
		FFmpegLogLevel: rt.FFmpegLogLevel,
	}

	// Initialize OpenWebIF Client and ReadyChecker for robust startup
	var checker ReadyChecker
	var owiHost string

	if cfg.ReceiverHost != "" {
		owiHost = cfg.ReceiverHost
	} else if cfg.TargetURL != "" {
		// Attempt to extract host from TargetURL
		if u, err := url.Parse(cfg.TargetURL); err == nil {
			owiHost = u.Hostname()
		}
	}

	if owiHost != "" {
		// Create OpenWebIF client
		// Port 0 uses default 8001/80 logic internally or we can specify if known.
		// Assuming standard Enigma2 setup where WebIF is on 80.
		// client.New expects base URL (e.g. http://host).
		owiBase := fmt.Sprintf("http://%s", owiHost)
		if !strings.Contains(owiHost, ":") {
			owiBase = fmt.Sprintf("http://%s:80", owiHost)
		}
		owiClient := openwebif.New(owiBase)
		// Decorate valid client with ReadyChecker
		checker = NewReadyChecker(owiClient, cfg.Logger.With().Str("component", "ready_check").Logger())
		hlsCfg.ReadyChecker = checker
		s.readyChecker = checker
	} else {
		cfg.Logger.Warn().Msg("could not determine receiver host for readiness checks; robust stream startup disabled")
	}

	hlsManager, err := NewHLSManager(cfg.Logger.With().Str("component", "hls").Logger(), hlsCfg)
	if err != nil {
		cfg.Logger.Warn().Err(err).Msg("failed to initialize HLS manager, HLS streaming disabled")
	} else {
		s.hlsManager = hlsManager
		cfg.Logger.Info().Msg("HLS streaming enabled")
	}

	r := apimw.NewRouter(apimw.StackConfig{
		EnableCORS:     true,
		AllowedOrigins: cfg.AllowedOrigins,
	})
	r.HandleFunc("/", s.handleRequest)
	r.HandleFunc("/*", s.handleRequest)
	r.NotFound(s.handleRequest)
	r.MethodNotAllowed(s.handleRequest)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadTimeout:       40 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	return s, nil
}

// GetSessions returns the list of active stream sessions.
func (s *Server) GetSessions() []*StreamSession {
	if s.registry == nil {
		return nil
	}
	return s.registry.List()
}

func (s *Server) validateUpstream(upstream string) error {
	// ALLOW: http://, https://
	// DENY: file://, user@host
	// HOST:PORT MUST MATCH configured allowedAuthorities EXACTLY

	u, err := url.Parse(upstream)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("disallowed scheme: %s", u.Scheme)
	}

	if u.User != nil {
		return fmt.Errorf("userinfo not allowed in upstream")
	}

	// Must have a port to match our strict config requirements
	if u.Port() == "" {
		return fmt.Errorf("upstream must have explicit port")
	}

	// Canonicalize authority (host:port)
	auth := canonicalizeAuthority(u.Host)

	// Strict Match
	if _, ok := s.allowedAuthorities[auth]; !ok {
		// No fuzzy matching, no localhost pivoting.
		// P3 Invariant: ONLY configured upstream.
		return fmt.Errorf("upstream authority %q not allowed (must match configured target exactly)", auth)
	}

	return nil
}

func canonicalizeAuthority(hostport string) string {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return strings.ToLower(hostport)
	}

	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			host = v4.String()
		} else {
			host = ip.String()
		}
	} else {
		host = strings.ToLower(host)
	}

	return net.JoinHostPort(host, port)
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	logger := xglog.WithContext(r.Context(), s.logger)

	// 1. Authentication Check
	// Policy: If auth is required (!AuthAnonymous), we enforce it strictly.
	// Since New() enforces that s.apiToken is set if !AuthAnonymous, checking !AuthAnonymous implies checking expectedToken presence.
	if !s.authAnonymous {
		if !auth.AuthorizeRequest(r, s.apiToken, true) {
			logger.Warn().
				Str("event", "auth.fail").
				Str("path", r.URL.Path).
				Str("reason", "unauthorized").
				Msg("proxy request unauthorized")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}
	// Security: Fail fast on disallowed upstream URL
	if r.URL.RawQuery != "" {
		if values, err := url.ParseQuery(r.URL.RawQuery); err == nil {
			if upstream := values.Get("upstream"); upstream != "" {
				if err := s.validateUpstream(upstream); err != nil {
					logger.Warn().Str("upstream", sanitizeURL(upstream)).Err(err).Msg("rejecting proxy request with disallowed upstream URL")
					http.Error(w, "Forbidden: disallowed upstream URL", http.StatusForbidden)
					return
				}
			}
		}
	}

	// Stop on limiter 429
	acquired, stop := s.acquireStreamSlotIfNeeded(w, r)
	if stop {
		return
	}
	if acquired {
		defer s.releaseStreamSlot()
	}

	// Session Tracking Upgrade
	var session *StreamSession

	if isStreamSessionStart(r) {
		// Identify Stream
		path := r.URL.Path
		serviceRef, _ := extractServiceRef(path)
		channelName := serviceRef // Default to ref

		// Try to look up name if possible?
		// For now simple ref is enough.

		// Note: isStream check logic was:
		// isStream := strings.HasPrefix(r.URL.Path, "/stream/") || strings.HasPrefix(r.URL.Path, "/api/stream.m3u")
		// But isStreamSessionStart() likely does similar.
		// Assuming isStreamSessionStart covers it.

		// If it's a playlist or direct stream (simple check to avoid segments)
		if strings.HasSuffix(r.URL.Path, ".m3u8") || !strings.Contains(r.URL.Path, ".") {
			// Create cancelable context for Terminate
			ctx, cancel := context.WithCancel(r.Context())
			r = r.WithContext(ctx)

			// Register
			session = s.registry.Register(r, channelName, serviceRef, nil)
			session.cancel = cancel

			defer s.registry.Unregister(session.ID)
		}
	}

	logger.Debug().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("remote_addr", r.RemoteAddr).
		Msg("proxy request")

	// Update activity on write
	if session != nil {
		w = newSessionWriter(w, session, logger)
	}

	if s.tryHandleHEAD(w, r) {
		return
	}

	if s.tryHandleHLS(w, r) {
		return
	}

	if s.tryHandleTranscode(w, r) {
		return
	}

	// Strict check: if we can't resolve the target, fail before proxying.
	// This prevents the "Director" from processing an empty/invalid URL.
	//
	// Note: In tests, Server can be manually constructed with a fixed reverse proxy and without
	// TargetURL/ReceiverHost. In that case, skip resolution and let the injected proxy handle it.
	if s.targetURL != nil || s.receiverHost != "" || s.playlistPath != "" {
		if _, ok := s.resolveTargetURL(r.Context(), r.URL.Path, r.URL.RawQuery); !ok {
			http.Error(w, "stream not found", http.StatusNotFound)
			return
		}
	}

	metrics.IncActiveStreams("direct")
	defer metrics.DecActiveStreams("direct")
	s.proxy.ServeHTTP(w, r)
}

// extractServiceRef extracts the service reference from a request path.
// extractServiceRef extracts the service reference and any path remainder from a request path.
// It normalizes /stream/{ref}/... and /hls/{ref}/... patterns.
func extractServiceRef(path string) (serviceRef, remainder string) {
	// 1. Handle /hls/ and /stream/ prefixes
	if strings.HasPrefix(path, "/hls/") || strings.HasPrefix(path, "/stream/") {
		trimmed := strings.TrimPrefix(path, "/hls/")
		trimmed = strings.TrimPrefix(trimmed, "/stream/")

		parts := strings.Split(trimmed, "/")
		if len(parts) > 0 {
			serviceRef = parts[0]
			// Handle legacy .m3u8 suffix on the ref itself (e.g. /hls/ref.m3u8)
			serviceRef = strings.TrimSuffix(serviceRef, ".m3u8")
		}
		if len(parts) > 1 {
			remainder = strings.Join(parts[1:], "/")
		}
		return serviceRef, remainder
	}

	// 2. Fallback for direct paths like /1:0:1... or simple identifiers
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 {
		serviceRef = parts[0]
	}
	if len(parts) > 1 {
		remainder = strings.Join(parts[1:], "/")
	}
	return serviceRef, remainder
}

// sessionWriter wraps ResponseWriter to update StreamSession.
type sessionWriter struct {
	http.ResponseWriter
	session  *StreamSession
	logger   zerolog.Logger
	wroteHdr bool
	mu       sync.Mutex
}

func newSessionWriter(w http.ResponseWriter, session *StreamSession, logger zerolog.Logger) *sessionWriter {
	return &sessionWriter{
		ResponseWriter: w,
		session:        session,
		logger:         logger,
	}
}

func (w *sessionWriter) WriteHeader(status int) {
	w.mu.Lock()
	w.wroteHdr = true
	w.mu.Unlock()
	w.ResponseWriter.WriteHeader(status)
}

func (w *sessionWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	if n > 0 {
		w.session.UpdateActivity(n)
	}
	return n, err
}

func (w *sessionWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *sessionWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack not supported")
}

func (s *Server) resolveTargetURL(ctx context.Context, path, rawQuery string) (string, bool) {
	logger := xglog.WithContext(ctx, s.logger)

	// Normalize path using robust extractor
	// This ensures we always look up the bare service reference or slug
	// and strips /stream/, /hls/, and handles remainder correctly.
	normalizedRef, remainder := extractServiceRef(path)

	// Reconstruct path non-lossily for downstream use (e.g. fallback target construction)
	// We want /{ref} or /{ref}/{remainder}
	normalizedPath := "/" + normalizedRef
	if remainder != "" {
		normalizedPath += "/" + remainder
	}
	path = normalizedPath

	serviceRef := normalizedRef
	isRef := strings.Contains(serviceRef, ":")

	if !isRef && serviceRef != "" {
		if streamURL, ok := s.lookupStreamURL(serviceRef); ok {
			if s.isSelfURL(streamURL) {
				if parsed, err := url.Parse(streamURL); err == nil {
					path = parsed.Path
					serviceRef = strings.TrimPrefix(path, "/")
					logger.Debug().Str("slug", serviceRef).Str("resolved_path", path).Msg("resolved slug to self-referencing proxy URL, extracting path")
				}
			} else {
				streamURL = appendRawQuery(streamURL, rawQuery)
				logger.Debug().Str("slug", serviceRef).Str("target", sanitizeURL(streamURL)).Msg("resolved slug to upstream URL via M3U")
				return streamURL, true
			}
		} else {
			if err := s.loadM3U(); err == nil {
				if streamURL, ok := s.lookupStreamURL(serviceRef); ok {
					streamURL = appendRawQuery(streamURL, rawQuery)
					logger.Info().Str("slug", serviceRef).Msg("resolved slug after M3U reload")
					return streamURL, true
				}
			}
		}
	}

	// Dynamic Upstream Override (e.g. for Recordings)
	if rawQuery != "" {
		values, _ := url.ParseQuery(rawQuery)
		if upstream := values.Get("upstream"); upstream != "" {
			if err := s.validateUpstream(upstream); err == nil {
				logger.Debug().Str("upstream", sanitizeURL(upstream)).Msg("using explicit upstream URL")
				return upstream, true
			}
			logger.Warn().Str("upstream", sanitizeURL(upstream)).Msg("ignoring disallowed upstream URL")
		}
	}

	if s.targetURL != nil {
		targetURL := s.targetURL.String() + path
		return appendRawQuery(targetURL, rawQuery), true
	}

	// Universal Fallback (WebAPI Resolution)
	// If the stream is not in our loaded M3U, it might still be a valid Service Reference.
	// Instead of guessing Port 8001 (which breaks encrypted channels), we ask the WebAPI.
	if serviceRef != "" && !strings.Contains(serviceRef, "/") {
		// Construct the WebAPI URL for this reference
		// Note: We don't have the full "Zap" logic here, just the URL construction.
		// BUT: resolveTargetURL is synchronous and expected to return a URL string.
		// We can't block for 5s here easily if this is called in Director.

		// BETTER APPROACH:
		// We construct a "Virtual" WebAPI URL for this service ref.
		// We trust the ZapAndResolveStream logic (called later by HLS handler) to handle the actual resolution.
		// However, for DIRECT proxying (Director), we need a simplified approach.

		// Wait, if we return a WebAPI URL here, the caller might use it as the upstream?
		// No, the caller expects a STREAM URL (http://ip:port/ref).

		// We must perform the resolution here if we want to be safe.
		// This might add latency to the first request, but it's better than broken streams.

		// Logic:
		// 1. If we have a TargetURL (Fixed mode), we shouldn't be here really unless playlist was missing?
		// 2. If we are in Receiver Host mode, we can try to ask the receiver.
		if s.receiverHost == "" {
			return "", false
		}

		webAPIURL := coreopenwebif.ConvertToWebAPI(
			fmt.Sprintf("http://%s:80", s.receiverHost), // Base URL
			serviceRef,
		)

		logger.Info().Str("service_ref", serviceRef).Msg("attempting dynamic resolution via WebAPI (not in playlist)")

		streamURL, _, err := ZapAndResolveStream(ctx, webAPIURL, serviceRef, s.readyChecker)
		if err == nil {
			logger.Info().Str("service_ref", serviceRef).Str("resolved_url", sanitizeURL(streamURL)).Msg("dynamically resolved stream URL")
			return appendRawQuery(streamURL, rawQuery), true
		}

		logger.Warn().Err(err).Str("service_ref", serviceRef).Msg("dynamic WebAPI resolution failed")
	}

	// NO BLIND FALLBACK to 8001.
	logger.Warn().Str("path", path).Msg("could not resolve stream URL (check playlist or upstream param)")
	return "", false
}

// acquireStreamSlotIfNeeded attempts to acquire a stream slot if the request is a session start.
// Returns (acquired, stop). If stop is true, an error (e.g. 429) has been written and the caller should return.
func (s *Server) acquireStreamSlotIfNeeded(w http.ResponseWriter, r *http.Request) (bool, bool) {
	if s.streamLimiter == nil {
		return false, false
	}
	if !isStreamSessionStart(r) {
		return false, false
	}
	if !s.streamLimiter.TryAcquire(1) {
		http.Error(w, "too many concurrent streams", http.StatusTooManyRequests)
		return false, true
	}
	return true, false
}

func (s *Server) releaseStreamSlot() {
	if s.streamLimiter != nil {
		s.streamLimiter.Release(1)
	}
}

func isStreamSessionStart(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path

	// 1. Exclude segments and media chunks (Limiter Safety)
	if strings.HasSuffix(path, ".ts") ||
		strings.HasSuffix(path, ".m4s") ||
		strings.HasSuffix(path, ".mp4") ||
		strings.HasSuffix(path, ".aac") {
		return false
	}

	// 2. Exclude HLS control endpoints
	if strings.HasSuffix(path, "/preflight") {
		return false
	}

	// 3. Count manifest requests
	if strings.HasSuffix(path, ".m3u8") {
		return true
	}

	// 4. Count direct /stream/ starts (legacy or direct)
	if strings.HasPrefix(path, "/stream/") {
		return true
	}

	// 5. Count direct service ref (/1:0:1:...)
	ref, _ := extractServiceRef(path)
	return ref != "" && strings.Contains(ref, ":")
}

func (s *Server) handleHLSRequest(w http.ResponseWriter, r *http.Request) {
	logger := xglog.WithContext(r.Context(), s.logger)

	serviceRef, targetURL, err := s.resolveHLS(r.Context(), r)
	if err != nil {
		logger.Warn().Err(err).Msg("HLS resolution failed")
		if strings.Contains(err.Error(), "unauthorized") { // Simplified check
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
		} else {
			http.Error(w, err.Error(), http.StatusNotFound)
		}
		return
	}

	path := r.URL.Path
	_, remainder := extractServiceRef(path)

	logger.Debug().
		Str("service_ref", serviceRef).
		Str("target", sanitizeURL(targetURL)).
		Str("path", path).
		Msg("serving HLS stream")

	if remainder == "preflight" || strings.HasSuffix(path, "/preflight") {
		// Use Preflight method directly (shared logic)
		status, err := s.Preflight(r.Context(), r, serviceRef)
		if err != nil {
			logger.Error().Err(err).Int("status", status).Str("service_ref", serviceRef).Msg("HLS preflight failed")
			http.Error(w, "HLS preflight failed", status)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(status)
		return
	}

	if err := s.hlsManager.ServeHLS(w, r, serviceRef, targetURL); err != nil {
		logger.Error().
			Err(err).
			Str("service_ref", serviceRef).
			Msg("HLS streaming failed")
		http.Error(w, "HLS streaming failed", http.StatusInternalServerError)
	}
}

// Preflight ensures the stream is ready (idempotent, coalesced, cached).
// Returns status code (204/503/etc) and potential error.
func (s *Server) Preflight(ctx context.Context, req *http.Request, serviceRef string) (int, error) {
	// 1. Robust Cache Key (Ref + Query Params)
	// Includes upstream, profile, etc. to prevent poisoning.
	cacheKey := serviceRef + "|" + req.URL.RawQuery

	// 2. Check Cache (TTL 10s)
	if val, ok := s.preflightCache.Load(cacheKey); ok {
		if entry, ok := val.(preflightCacheEntry); ok {
			if time.Since(entry.timestamp) < 10*time.Second {
				// Cache Hit
				if entry.status == http.StatusNoContent {
					return entry.status, nil
				}
				if entry.status == http.StatusNotFound {
					return entry.status, fmt.Errorf("%w: cached", ErrStreamNotFound)
				}
				// Respect TTL for failures too
				return entry.status, fmt.Errorf("cached preflight result: %d", entry.status)
			}
		} else {
			// Type mismatch? excessive defensive coding: delete it.
			s.preflightCache.Delete(cacheKey)
		}
	}

	// 3. Coalesce concurrent requests via singleflight
	// Key matches cache key for correct coalescing scope
	res, err, _ := s.sfg.Do(cacheKey, func() (any, error) {
		// Re-check cache inside lock? singleflight handles the "wait for result".

		// 4. Resolve Target (Validates Ref & Compute Target)
		// We use the extracted/resolved values from here on.
		resolvedRef, targetURL, err := s.resolveHLS(ctx, req)
		if err != nil {
			return http.StatusNotFound, err
		}

		// Verify resolvedRef matches passed serviceRef?
		// API passed serviceRef from path param.
		// resolveHLS extracts from path.
		// They should match. If not, something weird (traversal?).
		if resolvedRef != serviceRef {
			return http.StatusBadRequest, fmt.Errorf("service ref mismatch (path vs param)")
		}

		// 5. Delegate to HLS Manager (Actual Readiness Check)
		if err := s.hlsManager.PreflightHLS(ctx, req, resolvedRef, targetURL); err != nil {
			// Distinguish between Reference Error vs Not Ready vs Internal

			// P4c Hardening: Typed Errors
			if errors.Is(err, ErrReadyTimeout) || errors.Is(err, ErrNotReady) {
				return http.StatusServiceUnavailable, err
			}
			if errors.Is(err, ErrInvariant) {
				return http.StatusInternalServerError, err
			}

			// Fallback for wrapped errors or string-based ones from deeper layers
			errStr := err.Error()
			if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "not ready") {
				return http.StatusServiceUnavailable, err
			}

			// Default to 500
			return http.StatusInternalServerError, err
		}

		return http.StatusNoContent, nil
	})

	status := http.StatusServiceUnavailable
	if val, ok := res.(int); ok {
		status = val
	}

	// 6. Update Cache
	// P4c Hardening: Only cache Success (204) for full TTL (10s).
	// Do NOT cache failures (503) to allow immediate recovery once backend is ready.
	if status == http.StatusNoContent {
		s.preflightCache.Store(cacheKey, preflightCacheEntry{
			status:    status,
			timestamp: time.Now(),
		})
	} else if status == http.StatusNotFound {
		// 404 is a hard error (bad ref), we can cache it to prevent spam.
		s.preflightCache.Store(cacheKey, preflightCacheEntry{
			status:    status,
			timestamp: time.Now(),
		})
	}
	// 503 is transient (starting), do not cache.

	return status, err
}

// resolveHLS encapsulates the logic to parse a request, extract the service ref,
// and resolve the target URL. It includes basic validation but relies on upstream auth.
func (s *Server) resolveHLS(ctx context.Context, r *http.Request) (string, string, error) {
	path := r.URL.Path
	serviceRef, _ := extractServiceRef(path)

	if serviceRef == "" {
		return "", "", fmt.Errorf("service reference required")
	}

	targetURL, ok := s.resolveTargetURL(ctx, "/"+serviceRef, r.URL.RawQuery)
	if !ok {
		return "", "", fmt.Errorf("%w: service reference not found in playlist", ErrStreamNotFound)
	}

	return serviceRef, targetURL, nil
}

func (s *Server) Start() error {
	logEvent := s.logger.Info().Str("addr", s.addr)

	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.monitorCancel = monitorCancel
	s.mu.Unlock()
	s.startIdleMonitor(monitorCtx)

	if s.targetURL != nil {
		logEvent.Str("target", sanitizeURL(s.targetURL.String()))
	} else if s.receiverHost != "" {
		logEvent.Str("receiver", s.receiverHost).Str("mode", "receiver_fallback")
	}

	if s.tlsCert != "" && s.tlsKey != "" {
		logEvent.Msg("starting stream proxy server (HTTPS)")
		if err := s.httpServer.ListenAndServeTLS(s.tlsCert, s.tlsKey); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy server (HTTPS) failed: %w", err)
		}
	} else {
		logEvent.Msg("starting stream proxy server (HTTP)")
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("proxy server (HTTP) failed: %w", err)
		}
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info().Msg("shutting down stream proxy server")

	if s.hlsManager != nil {
		s.hlsManager.Shutdown()
	}

	s.mu.Lock()
	if s.monitorCancel != nil {
		s.monitorCancel()
		s.monitorCancel = nil
	}
	s.mu.Unlock()

	if s.proxy != nil && s.proxy.Transport != nil {
		if t, ok := s.proxy.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}

	return s.httpServer.Shutdown(ctx)
}

func (s *Server) loadM3U() error {
	if s.playlistPath == "" {
		return nil
	}

	data, err := os.ReadFile(s.playlistPath)
	if err != nil {
		return fmt.Errorf("read playlist: %w", err)
	}

	channels := m3u.Parse(string(data))
	newMap := make(map[string]string)

	for _, ch := range channels {
		id := ch.TvgID
		if id != "" && ch.URL != "" {
			newMap[id] = ch.URL
		}
	}
	s.logger.Info().Int("count", len(newMap)).Str("path", s.playlistPath).Msg("loaded channels from playlist")
	s.channelMu.Lock()
	s.channelMap = newMap
	s.channelMu.Unlock()

	return nil
}

func (s *Server) lookupStreamURL(id string) (string, bool) {
	s.channelMu.RLock()
	url, ok := s.channelMap[id]
	s.channelMu.RUnlock()
	return url, ok
}

func appendRawQuery(base, rawQuery string) string {
	if rawQuery == "" {
		return base
	}

	u, err := url.Parse(base)
	if err != nil {
		if strings.Contains(base, "?") {
			return base + "&" + rawQuery
		}
		return base + "?" + rawQuery
	}

	extra, err := url.ParseQuery(rawQuery)
	if err != nil {
		if u.RawQuery == "" {
			u.RawQuery = rawQuery
		} else {
			u.RawQuery = u.RawQuery + "&" + rawQuery
		}
		return u.String()
	}

	values := u.Query()
	for key, vs := range extra {
		for _, v := range vs {
			values.Add(key, v)
		}
	}
	u.RawQuery = values.Encode()
	return u.String()
}

func splitListenAddr(addr string) (string, string) {
	if addr == "" {
		return "", ""
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", strings.TrimPrefix(addr, ":")
	}
	if port == "" {
		port = strings.TrimPrefix(addr, ":")
	}
	return host, port
}

func collectLocalHosts(explicitHost string) map[string]struct{} {
	hosts := map[string]struct{}{
		"localhost": {},
		"127.0.0.1": {},
		"::1":       {},
	}

	addHost := func(host string) {
		if host == "" {
			return
		}
		hosts[strings.ToLower(host)] = struct{}{}
	}

	if explicitHost != "" && explicitHost != "0.0.0.0" && explicitHost != "::" {
		addHost(explicitHost)
	}

	if hn, err := os.Hostname(); err == nil {
		addHost(hn)
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				switch v := addr.(type) {
				case *net.IPNet:
					addHost(v.IP.String())
				case *net.IPAddr:
					addHost(v.IP.String())
				default:
					addHost(addr.String())
				}
			}
		}
	}

	return hosts
}

func (s *Server) isSelfURL(streamURL string) bool {
	if s.listenPort == "" {
		return false
	}

	parsed, err := url.Parse(streamURL)
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port == "" {
		switch parsed.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}

	if port != s.listenPort {
		return false
	}

	if host == "" {
		return true
	}

	_, ok := s.localHosts[host]
	return ok
}

func (s *Server) startIdleMonitor(ctx context.Context) {
	if s.idleTimeout <= 0 {
		return
	}

	s.logger.Info().Dur("timeout", s.idleTimeout).Msg("starting centralized stream idle monitor")

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now()
				timeout := s.idleTimeout

				// Validate sessions from Registry, not sync.Map
				sessions := s.registry.List()
				for _, sess := range sessions {
					last := sess.LastActivity()
					if now.Sub(last) > timeout {
						s.logger.Warn().
							Str("session_id", sess.ID).
							Dur("last_activity", now.Sub(last)).
							Msg("stream idle timeout reached, cancelling")

						sess.cancel()
					}
				}
			}
		}
	}()
}
