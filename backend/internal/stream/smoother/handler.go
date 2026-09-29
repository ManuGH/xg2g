// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package smoother

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
)

// FlusherWriter wraps an http.ResponseWriter and http.Flusher.
type FlusherWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// Write writes data and immediately flushes the TCP socket.
func (fw *FlusherWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err == nil && fw.flusher != nil {
		fw.flusher.Flush()
	}
	return n, err
}

// Handler serves paced, smoothed TS streams from upstream sources (local Enigma2 tuners or network relays).
type Handler struct {
	receiverHost string
	streamPort   int
	cfg          Config
	client       *http.Client
}

// isValidServiceRef validates that serviceRef conforms strictly to DVB/Enigma2
// service reference syntax and rejects path traversal, authority injection, or query strings.
func isValidServiceRef(ref string) bool {
	if ref == "" || len(ref) > 256 {
		return false
	}
	if strings.ContainsAny(ref, "/\\?#@ \t\r\n\x00") || strings.Contains(ref, "..") {
		return false
	}
	for _, r := range ref {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ':' || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// decodeRelayURL parses a relay target from base64, base64url, or plain URL.
func decodeRelayURL(encoded string) (string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", errors.New("empty relay target")
	}

	// Try base64url (unpadded or padded)
	if b, err := base64.RawURLEncoding.DecodeString(encoded); err == nil && len(b) > 0 {
		return string(b), nil
	}
	if b, err := base64.URLEncoding.DecodeString(encoded); err == nil && len(b) > 0 {
		return string(b), nil
	}
	if b, err := base64.StdEncoding.DecodeString(encoded); err == nil && len(b) > 0 {
		return string(b), nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(encoded); err == nil && len(b) > 0 {
		return string(b), nil
	}

	// Direct URL fallback if unencoded
	if strings.HasPrefix(encoded, "http://") || strings.HasPrefix(encoded, "https://") {
		return encoded, nil
	}

	return "", errors.New("invalid relay target encoding: expected base64url or http(s) URL")
}

// validateRelayURL checks that the target URL has a valid scheme and host.
func validateRelayURL(raw string) (string, error) {
	if len(raw) > 2048 {
		return "", errors.New("relay target URL exceeds maximum length (2048 bytes)")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("malformed relay URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported relay scheme %q (only http and https supported)", u.Scheme)
	}
	if u.Hostname() == "" {
		return "", errors.New("missing hostname in relay URL")
	}
	return u.String(), nil
}

// NewHandler creates a new TS smoothing HTTP handler.
func NewHandler(receiverBaseURL string, streamPort int, cfg Config) *Handler {
	host := "10.10.55.64"
	if receiverBaseURL != "" {
		if u, err := url.Parse(receiverBaseURL); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		}
	}
	if streamPort <= 0 {
		streamPort = 8001
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives:     true,
	}

	return &Handler{
		receiverHost: host,
		streamPort:   streamPort,
		cfg:          cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   0, // continuous streaming
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("stopped after 10 redirects")
				}
				if len(via) > 0 {
					if ua := via[0].Header.Get("User-Agent"); ua != "" {
						req.Header.Set("User-Agent", ua)
					}
				}
				return nil
			},
		},
	}
}

// ServeHTTP handles GET /api/v3/stream/smooth/* requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	const prefix = "/api/v3/stream/smooth/"
	targetParam := strings.TrimPrefix(path, prefix)
	if targetParam == "" || targetParam == path {
		targetParam = r.URL.Query().Get("sref")
	}

	var (
		targetURL     string
		isRelay       bool
		transcodeMode string
		sessionRef    string
	)

	// Check if this is a transcode or stream relay request
	if strings.HasPrefix(targetParam, "transcode/") {
		isRelay = true
		transcodeMode = "hevc"
		encodedTarget := strings.TrimPrefix(targetParam, "transcode/")
		decoded, err := decodeRelayURL(encodedTarget)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid transcode target: %v", err), http.StatusBadRequest)
			return
		}
		validated, err := validateRelayURL(decoded)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid transcode URL: %v", err), http.StatusBadRequest)
			return
		}
		targetURL = validated
		sessionRef = "transcode:" + targetURL
	} else if strings.HasPrefix(targetParam, "relay/") {
		isRelay = true
		encodedTarget := strings.TrimPrefix(targetParam, "relay/")
		decoded, err := decodeRelayURL(encodedTarget)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid relay target: %v", err), http.StatusBadRequest)
			return
		}
		validated, err := validateRelayURL(decoded)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid relay URL: %v", err), http.StatusBadRequest)
			return
		}
		targetURL = validated
		sessionRef = "relay:" + targetURL
	} else if b64 := r.URL.Query().Get("b64"); b64 != "" {
		isRelay = true
		decoded, err := decodeRelayURL(b64)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid b64 relay target: %v", err), http.StatusBadRequest)
			return
		}
		validated, err := validateRelayURL(decoded)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid relay URL: %v", err), http.StatusBadRequest)
			return
		}
		targetURL = validated
		sessionRef = "relay:" + targetURL
	} else if rawURL := r.URL.Query().Get("url"); rawURL != "" {
		isRelay = true
		validated, err := validateRelayURL(rawURL)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid relay URL: %v", err), http.StatusBadRequest)
			return
		}
		targetURL = validated
		sessionRef = "relay:" + targetURL
	} else {
		// Standard Enigma2 DVB service reference
		serviceRef := targetParam
		if serviceRef == "" {
			http.Error(w, "missing serviceRef in stream path", http.StatusBadRequest)
			return
		}

		if unescaped, err := url.PathUnescape(serviceRef); err == nil {
			serviceRef = unescaped
		}

		if !isValidServiceRef(serviceRef) {
			http.Error(w, "invalid serviceRef: path traversal or invalid characters detected", http.StatusBadRequest)
			return
		}

		targetURL = fmt.Sprintf("http://%s:%d/%s", h.receiverHost, h.streamPort, serviceRef)
		sessionRef = serviceRef
	}

	if qm := r.URL.Query().Get("transcode"); qm != "" {
		transcodeMode = qm
	}

	logger := log.L().With().
		Str("sessionRef", sessionRef).
		Str("targetURL", targetURL).
		Bool("isRelay", isRelay).
		Str("transcodeMode", transcodeMode).
		Float64("reservoirMs", h.cfg.StartupReservoirMs).
		Logger()

	logger.Info().Msg("starting smoothed TS stream session")

	// Use streaming-compatible User-Agent for upstream network streams
	ua := r.UserAgent()
	if ua == "" || strings.HasPrefix(ua, "Go-http-client") {
		ua = "IPTVSmartersPro/1.0.0 (Linux; Android)"
	}

	// Live hardware/software transcoding pipeline
	if transcodeMode != "" {
		logger.Info().Str("mode", transcodeMode).Msg("starting live hardware transcoding pipeline")

		transcoderIn, err := StartTranscoder(r.Context(), targetURL, transcodeMode, ua)
		if err != nil {
			logger.Error().Err(err).Msg("failed to start live transcoder")
			http.Error(w, fmt.Sprintf("transcode failed: %v", err), http.StatusInternalServerError)
			return
		}
		defer func() { _ = transcoderIn.Close() }()

		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Connection", "close")
		w.Header().Set("X-Smoother-Reservoir-Ms", fmt.Sprintf("%.0f", h.cfg.StartupReservoirMs))
		w.WriteHeader(http.StatusOK)

		var outWriter io.Writer = w
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
			outWriter = &FlusherWriter{w: w, flusher: flusher}
		}

		report, err := SmoothStream(r.Context(), transcoderIn, outWriter, h.cfg)
		if err != nil && r.Context().Err() == nil {
			logger.Warn().Err(err).Msg("transcoded smoothed stream terminated with error")
		} else if report != nil {
			logger.Info().
				Float64("durationSec", report.DurationSeconds).
				Int64("packetsOut", report.OutputPackets).
				Int64("repairedCCs", report.CCErrorsRepaired).
				Float64("firstByteDelayMs", report.FirstByteDelayMs).
				Float64("steadyStateLagMs", report.SteadyStateDelayMs).
				Msg("transcoded smoothed TS session finished cleanly")
		}
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, targetURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create upstream request: %v", err), http.StatusInternalServerError)
		return
	}

	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")

	client := h.client
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
			Timeout: 0,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to connect to upstream source")
		http.Error(w, fmt.Sprintf("upstream source unavailable: %v", err), http.StatusBadGateway)
		return
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		logger.Warn().Int("status", resp.StatusCode).Msg("upstream source returned non-200")
		http.Error(w, fmt.Sprintf("upstream error: %d %s", resp.StatusCode, resp.Status), resp.StatusCode)
		return
	}

	// Prepare streaming response headers
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Connection", "close")
	w.Header().Set("X-Smoother-Reservoir-Ms", fmt.Sprintf("%.0f", h.cfg.StartupReservoirMs))
	w.WriteHeader(http.StatusOK)

	var outWriter io.Writer = w
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
		outWriter = &FlusherWriter{w: w, flusher: flusher}
	}

	resilientIn := NewResilientUpstreamReader(
		r.Context(),
		client,
		targetURL,
		req.Header,
		resp.Body,
		DefaultResilientConfig(),
	)
	defer resilientIn.Close()

	report, err := SmoothStream(r.Context(), resilientIn, outWriter, h.cfg)
	if err != nil && r.Context().Err() == nil {
		logger.Warn().Err(err).Msg("smoothed stream terminated with error")
	} else if report != nil {
		logger.Info().
			Float64("durationSec", report.DurationSeconds).
			Int64("packetsOut", report.OutputPackets).
			Int64("underruns", report.Underruns).
			Int64("repairedCCs", report.CCErrorsRepaired).
			Int64("upstreamReconnects", resilientIn.ReconnectCount).
			Float64("firstByteDelayMs", report.FirstByteDelayMs).
			Float64("steadyStateLagMs", report.SteadyStateDelayMs).
			Msg("smoothed TS session finished cleanly")
	}
}
