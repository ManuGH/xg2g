// SPDX-License-Identifier: MIT
package openwebif

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog"
)

type Client struct {
	base       string
	port       int
	http       *http.Client
	log        zerolog.Logger
	host       string
	timeout    time.Duration
	maxRetries int
	backoff    time.Duration
	maxBackoff time.Duration
	username   string
	password   string
}

type Options struct {
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	MaxRetries            int
	Backoff               time.Duration
	MaxBackoff            time.Duration
	Username              string
	Password              string
}

const (
	defaultStreamPort = 8001
	defaultTimeout    = 10 * time.Second
	defaultRetries    = 3
	defaultBackoff    = 500 * time.Millisecond
	maxTimeout        = 60 * time.Second
	maxRetries        = 10
	maxBackoff        = 30 * time.Second
)

var (
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_openwebif_request_duration_seconds",
		Help:    "Duration of OpenWebIF HTTP requests per attempt",
		Buckets: prometheus.ExponentialBuckets(0.05, 2.0, 8),
	}, []string{"operation", "status", "attempt"})

	requestRetries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_request_retries_total",
		Help: "Number of OpenWebIF request retries performed",
	}, []string{"operation"})

	requestFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_request_failures_total",
		Help: "Number of failed OpenWebIF requests by error class",
	}, []string{"operation", "error_class"})

	requestSuccess = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_openwebif_request_success_total",
		Help: "Number of successful OpenWebIF requests",
	}, []string{"operation"})
)

// ClientInterface defines the subset used by other packages and tests.
type ClientInterface interface {
	Bouquets(ctx context.Context) (map[string]string, error)
	Services(ctx context.Context, bouquetRef string) ([][2]string, error)
	StreamURL(ref, name string) (string, error)
}

func New(base string) *Client {
	return NewWithPort(base, 0, Options{})
}

func NewWithPort(base string, streamPort int, opts Options) *Client {
	trimmedBase := strings.TrimRight(strings.TrimSpace(base), "/")
	port := resolveStreamPort(streamPort)
	host := extractHost(trimmedBase)
	logger := xglog.WithComponent("openwebif").With().Str("host", host).Logger()
	nopts := normalizeOptions(opts)

	dialerTimeout := 5 * time.Second
	tlsHandshakeTimeout := 5 * time.Second
	responseHeaderTimeout := 10 * time.Second
	if opts.ResponseHeaderTimeout > 0 {
		responseHeaderTimeout = opts.ResponseHeaderTimeout
	}

	// Resolve transport pool knobs from environment (optional overrides)
	maxIdleConns := getenvInt("XG2G_HTTP_MAX_IDLE_CONNS", 100)
	maxIdlePerHost := getenvInt("XG2G_HTTP_MAX_IDLE_CONNS_PER_HOST", 20)
	maxConnsPerHost := getenvInt("XG2G_HTTP_MAX_CONNS_PER_HOST", 50)
	idleConnTimeout := getenvDuration("XG2G_HTTP_IDLE_TIMEOUT", 90*time.Second)
	forceHTTP2 := strings.ToLower(os.Getenv("XG2G_HTTP_ENABLE_HTTP2")) != "false"

	// Create a dedicated, hardened transport with optimized pooling.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   dialerTimeout, // Connection timeout
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout, // Time to receive headers

		// Connection pooling / reuse
		DisableKeepAlives:   false,        // allow keep-alive
		ForceAttemptHTTP2:   forceHTTP2,   // prefer HTTP/2 when enabled
		MaxIdleConns:        maxIdleConns, // global idle pool
		MaxIdleConnsPerHost: maxIdlePerHost,
		MaxConnsPerHost:     maxConnsPerHost, // cap total per-host conns to avoid exhaustion
		IdleConnTimeout:     idleConnTimeout,
	}

	// Create a client with the hardened transport.
	// The per-request timeout is handled by the context passed to Do().
	hardenedClient := &http.Client{
		Transport: transport,
		// Safety net: overall cap per attempt to prevent slow body hangs.
		Timeout: 30 * time.Second,
	}

	return &Client{
		base:       trimmedBase,
		port:       port,
		http:       hardenedClient,
		log:        logger,
		host:       host,
		timeout:    nopts.Timeout,
		maxRetries: nopts.MaxRetries,
		backoff:    nopts.Backoff,
		maxBackoff: nopts.MaxBackoff,
		username:   opts.Username,
		password:   opts.Password,
	}
}

// getenvInt returns an int from ENV or the default if unset/invalid.
func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

// getenvDuration returns a duration from ENV or the default.
func getenvDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func normalizeOptions(opts Options) Options {
	if opts.Timeout <= 0 || opts.Timeout > maxTimeout {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = defaultRetries
	}
	if opts.MaxRetries > maxRetries {
		opts.MaxRetries = maxRetries
	}
	if opts.Backoff <= 0 {
		opts.Backoff = defaultBackoff
	}
	if opts.Backoff > maxBackoff {
		opts.Backoff = maxBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 2 * time.Second
	}
	if opts.MaxBackoff > maxBackoff {
		opts.MaxBackoff = maxBackoff
	}
	// Ensure MaxBackoff >= Backoff
	if opts.MaxBackoff < opts.Backoff {
		opts.MaxBackoff = opts.Backoff
	}
	return opts
}
func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	const path = "/api/bouquets"
	body, err := c.get(ctx, path, "bouquets", nil)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Bouquets [][]string `json:"bouquets"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		c.loggerFor(ctx).Error().Err(err).Str("event", "openwebif.decode").Str("operation", "bouquets").Msg("failed to decode bouquets response")
		return nil, err
	}

	out := make(map[string]string, len(payload.Bouquets))
	for _, b := range payload.Bouquets {
		if len(b) == 2 {
			out[b[1]] = b[0]
		} // name -> ref
	}
	c.loggerFor(ctx).Info().Str("event", "openwebif.bouquets").Int("count", len(out)).Msg("fetched bouquets")
	return out, nil
}

// /api/bouquets: [["<ref>","<name>"], ...]

type svcPayload struct {
	Services []struct {
		ServiceName string `json:"servicename"`
		ServiceRef  string `json:"servicereference"`
	} `json:"services"`
}

// svcPayloadFlat models the getallservices shape where the bouquet container
// contains a "subservices" array with the actual channel entries.
type svcPayloadFlat struct {
	Services []struct {
		ServiceName string `json:"servicename"`
		ServiceRef  string `json:"servicereference"`
		Subservices []struct {
			ServiceName string `json:"servicename"`
			ServiceRef  string `json:"servicereference"`
		} `json:"subservices"`
	} `json:"services"`
}

// EPGEvent represents a single programme entry from OpenWebIF EPG API
type EPGEvent struct {
	ID          int    `json:"id"` // Changed from string to int
	Title       string `json:"title"`
	Description string `json:"shortdesc"`
	LongDesc    string `json:"longdesc"`
	Begin       int64  `json:"begin_timestamp"`
	Duration    int64  `json:"duration_sec"`
	SRef        string `json:"sref"`
}

// EPGResponse represents the OpenWebIF EPG API response structure
type EPGResponse struct {
	Events []EPGEvent `json:"events"`
	Result bool       `json:"result"`
}

func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	maskedRef := maskValue(bouquetRef)
	decorate := func(zc *zerolog.Context) {
		zc.Str("bouquet_ref", maskedRef)
	}
	try := func(urlPath, operation string) ([][2]string, error) {
		body, err := c.get(ctx, urlPath, operation, decorate)
		if err != nil {
			return nil, err
		}

		// For flat endpoint, decode directly into svcPayloadFlat to preserve subservices
		if operation == "services.flat" {
			var flat svcPayloadFlat
			if err := json.Unmarshal(body, &flat); err != nil {
				c.loggerFor(ctx).Error().Err(err).
					Str("event", "openwebif.decode").
					Str("operation", operation).
					Str("bouquet_ref", maskedRef).
					Msg("failed to decode services response (flat)")
				return nil, err
			}
			out := make([][2]string, 0, len(flat.Services)*4)
			for _, s := range flat.Services {
				// Check if this is a bouquet container with subservices
				if len(s.Subservices) > 0 {
					c.loggerFor(ctx).Debug().
						Str("container", s.ServiceName).
						Int("subservices_count", len(s.Subservices)).
						Msg("expanding bouquet container")
					for _, ch := range s.Subservices {
						// Skip any nested containers or invalid entries
						if strings.HasPrefix(ch.ServiceRef, "1:7:") || ch.ServiceRef == "" {
							continue
						}
						out = append(out, [2]string{ch.ServiceName, ch.ServiceRef})
					}
				} else if !strings.HasPrefix(s.ServiceRef, "1:7:") && s.ServiceRef != "" {
					// Regular service (not a container)
					out = append(out, [2]string{s.ServiceName, s.ServiceRef})
				}
			}
			return out, nil
		}

		// Nested endpoint: standard decode
		var p svcPayload
		if err := json.Unmarshal(body, &p); err != nil {
			c.loggerFor(ctx).Error().Err(err).
				Str("event", "openwebif.decode").
				Str("operation", operation).
				Str("bouquet_ref", maskedRef).
				Msg("failed to decode services response")
			return nil, err
		}
		out := make([][2]string, 0, len(p.Services))
		for _, s := range p.Services {
			// 1:7:* = Bouquet-Container; skip these in nested endpoint
			if strings.HasPrefix(s.ServiceRef, "1:7:") {
				continue
			}
			out = append(out, [2]string{s.ServiceName, s.ServiceRef})
		}
		return out, nil
	}

	// Try bouquet-specific endpoint (more reliable than getallservices)
	if out, err := try("/api/getservices?sRef="+url.QueryEscape(bouquetRef), "services.nested"); err == nil && len(out) > 0 {
		c.loggerFor(ctx).Info().Str("event", "openwebif.services").Str("bouquet_ref", maskedRef).Int("count", len(out)).Msg("fetched services via nested endpoint")
		return out, nil
	}
	c.loggerFor(ctx).Warn().Str("event", "openwebif.services").Str("bouquet_ref", maskedRef).Msg("no services found for bouquet")
	return [][2]string{}, nil
}

func (c *Client) StreamURL(ref, name string) (string, error) {
	base := strings.TrimSpace(c.base)
	if base == "" {
		return "", fmt.Errorf("openwebif base URL is empty")
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse openwebif base URL %q: %w", base, err)
	}

	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}

	host := parsed.Host
	if host == "" {
		return "", fmt.Errorf("openwebif base URL %q missing host", base)
	}

	// Check if WebIF streaming should be used (works in standby mode)
	// Set XG2G_USE_WEBIF_STREAMS=true to use /web/stream.m3u URLs
	useWebIF := os.Getenv("XG2G_USE_WEBIF_STREAMS") == "true"

	if useWebIF {
		// Use WebIF streaming endpoint (works in standby mode)
		// Format: http://<host>/web/stream.m3u?ref=<service_ref>&name=<channel_name>
		hostname := parsed.Hostname()
		if hostname == "" {
			return "", fmt.Errorf("openwebif base URL %q missing hostname", base)
		}

		// Use base URL port (typically 80) for WebIF
		basePort := parsed.Port()
		if basePort != "" {
			host = net.JoinHostPort(hostname, basePort)
		} else {
			host = hostname
		}

		u := &url.URL{
			Scheme:   parsed.Scheme,
			Host:     host,
			Path:     "/web/stream.m3u",
			RawQuery: fmt.Sprintf("ref=%s&name=%s", url.QueryEscape(ref), url.QueryEscape(name)),
		}
		return u.String(), nil
	}

	// Use direct TS streaming (works best with IPTV players when receiver is active)
	// Format: http://<host>:<stream_port>/<service_ref>
	// This is the direct MPEG-TS stream from the Enigma2 receiver

	// If base URL already has a port, preserve it
	// Otherwise, add the stream port
	_, existingPort, err := net.SplitHostPort(host)
	if err != nil || existingPort == "" {
		// No port in base URL, add stream port
		hostname := parsed.Hostname()
		if hostname == "" {
			return "", fmt.Errorf("openwebif base URL %q missing hostname", base)
		}

		streamPort := c.port
		if streamPort <= 0 {
			streamPort = 8001 // Default Enigma2 stream port
		}
		host = net.JoinHostPort(hostname, strconv.Itoa(streamPort))
	}

	u := &url.URL{
		Scheme: parsed.Scheme,
		Host:   host,
		Path:   "/" + ref,
	}

	return u.String(), nil
}

// Preserved helper (now uses New, so ENV variables take effect)
func StreamURL(base, ref, name string) (string, error) {
	return NewWithPort(base, 0, Options{}).StreamURL(ref, name)
}

func resolveStreamPort(port int) int {
	if port > 0 && port <= 65535 {
		return port
	}
	if v := os.Getenv("XG2G_STREAM_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p <= 65535 {
			return p
		}
	}
	return defaultStreamPort
}

func (c *Client) backoffDuration(attempt int) time.Duration {
	if c.backoff <= 0 {
		return 0
	}
	factor := 1 << (attempt - 1)
	d := time.Duration(factor) * c.backoff
	if d > c.maxBackoff {
		d = c.maxBackoff
	}
	return d
}

func shouldRetry(status int, err error) bool {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return netErr.Timeout()
		}
		return true
	}
	if status == http.StatusTooManyRequests {
		return true
	}
	if status >= 500 {
		return true
	}
	return false
}

func classifyError(err error, status int) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			if netErr.Timeout() {
				return "timeout"
			}
			return "network"
		}
		return "error"
	}
	if status >= 500 {
		return "http_5xx"
	}
	if status >= 400 {
		return "http_4xx"
	}
	if status == 0 {
		return "unknown"
	}
	return "ok"
}

func wrapError(operation string, err error, status int) error {
	if err != nil {
		return fmt.Errorf("%s request failed: %w", operation, err)
	}
	if status > 0 {
		return fmt.Errorf("%s: HTTP %d", operation, status)
	}
	return fmt.Errorf("%s: unknown error", operation)
}

func logPath(path string) string {
	endpoint := path
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	return endpoint
}

func (c *Client) logAttempt(
	ctx context.Context,
	operation string,
	path string,
	attempt, maxAttempts, status int,
	duration time.Duration,
	err error,
	errClass string,
	retry bool,
	decorate func(*zerolog.Context),
) {
	builder := c.loggerFor(ctx).With().
		Str("event", "openwebif.request").
		Str("operation", operation).
		Str("method", http.MethodGet).
		Str("endpoint", logPath(path)).
		Int("attempt", attempt).
		Int("max_attempts", maxAttempts).
		Int64("duration_ms", duration.Milliseconds()).
		Str("error_class", errClass)
	if status > 0 {
		builder = builder.Int("status", status)
	}
	if decorate != nil {
		decorate(&builder)
	}
	logger := builder.Logger()
	if err == nil && status == http.StatusOK {
		logger.Info().Msg("openwebif request completed")
		return
	}
	if retry {
		logger.Warn().Err(err).Msg("openwebif request retry")
		return
	}
	logger.Error().Err(err).Msg("openwebif request failed")
}

func recordAttemptMetrics(operation string, attempt, status int, duration time.Duration, success bool, errClass string, retry bool) {
	statusLabel := "0"
	if status > 0 {
		statusLabel = strconv.Itoa(status)
	}
	requestDuration.WithLabelValues(operation, statusLabel, strconv.Itoa(attempt)).Observe(duration.Seconds())
	if success {
		requestSuccess.WithLabelValues(operation).Inc()
		return
	}
	requestFailures.WithLabelValues(operation, errClass).Inc()
	if retry {
		requestRetries.WithLabelValues(operation).Inc()
	}
}

func (c *Client) get(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) ([]byte, error) {
	maxAttempts := c.maxRetries + 1
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var res *http.Response
		var err error
		var status int
		var duration time.Duration
		var data []byte

		func() {
			attemptCtx := ctx
			var cancel context.CancelFunc
			if c.timeout > 0 {
				attemptCtx, cancel = context.WithTimeout(ctx, c.timeout)
				defer cancel() // Ensure cancel is always called
			}

			req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodGet, c.base+path, nil)
			if reqErr != nil {
				c.loggerFor(ctx).Error().Err(reqErr).
					Str("event", "openwebif.request.build").
					Str("operation", operation).
					Msg("failed to build OpenWebIF request")
				err = reqErr
				return
			}

			// Add HTTP Basic Auth if credentials are provided
			if c.username != "" && c.password != "" {
				req.SetBasicAuth(c.username, c.password)
			}

			start := time.Now()
			res, err = c.http.Do(req)
			duration = time.Since(start)
			if res != nil {
				status = res.StatusCode
			}

			if err == nil && status == http.StatusOK {
				// Read body fully while attemptCtx is still active
				var readErr error

				// Check Content-Type header for charset
				contentType := res.Header.Get("Content-Type")

				// Read raw bytes first
				rawData, readErr := io.ReadAll(res.Body)
				if readErr != nil {
					err = fmt.Errorf("read response body: %w", readErr)
				} else {
					// Convert from ISO-8859-1/Latin-1 to UTF-8 if needed
					// Many OpenWebIF implementations send ISO-8859-1 but don't declare it properly
					// Check if data looks like it might be ISO-8859-1 encoded
					if needsLatin1Conversion(rawData, contentType) {
						data = convertLatin1ToUTF8(rawData)
					} else {
						data = rawData
					}
				}
			}
		}()

		// Handle early return from request building
		if err != nil && res == nil {
			return nil, err
		}

		errClass := classifyError(err, status)
		retry := attempt < maxAttempts && shouldRetry(status, err)
		c.logAttempt(ctx, operation, path, attempt, maxAttempts, status, duration, err, errClass, retry, decorate)
		recordAttemptMetrics(operation, attempt, status, duration, err == nil && status == http.StatusOK, errClass, retry)

		if err == nil && status == http.StatusOK {
			// Ensure body is closed now that we've read it
			if res != nil && res.Body != nil {
				closeBody(res.Body)
			}
			return data, nil
		}

		if res != nil {
			closeBody(res.Body)
		}

		lastErr = wrapError(operation, err, status)
		lastStatus = status

		if !retry {
			break
		}

		time.Sleep(c.backoffDuration(attempt))
	}
	return nil, wrapError(operation, lastErr, lastStatus)
}

func (c *Client) loggerFor(ctx context.Context) *zerolog.Logger {
	logger := xglog.WithContext(ctx, c.log)
	return &logger
}

func closeBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	if err := body.Close(); err != nil {
		// best effort; nothing to do
		_ = err
	}
}

func extractHost(base string) string {
	if base == "" {
		return ""
	}
	if strings.Contains(base, "://") {
		if u, err := url.Parse(base); err == nil && u.Host != "" {
			return u.Host
		}
	}
	if idx := strings.Index(base, "/"); idx >= 0 {
		return base[:idx]
	}
	return base
}

func maskValue(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "***"
	}
	if len(v) <= 8 {
		return v[:2] + "***" + v[len(v)-2:]
	}
	return v[:4] + "***" + v[len(v)-3:]
}

// GetEPG retrieves EPG data for a specific service reference over specified days
func (c *Client) GetEPG(ctx context.Context, sRef string, days int) ([]EPGEvent, error) {
	if days < 1 || days > 14 {
		return nil, fmt.Errorf("invalid EPG days: %d (must be 1-14)", days)
	}

	// Note: OpenWebIF /web/epgservice doesn't properly support endTime parameter
	// Using time=-1 returns all available EPG data (typically 7-14 days)
	// We rely on the receiver's EPG database having sufficient data

	// Try primary endpoint: /api/epgservice
	primaryURL := fmt.Sprintf("/api/epgservice?sRef=%s&time=-1",
		url.QueryEscape(sRef))

	events, err := c.fetchEPGFromURL(ctx, primaryURL)
	if err == nil && len(events) > 0 {
		return events, nil
	}

	// Log primary failure and try fallback
	c.log.Debug().
		Err(err).
		Str("sref", maskValue(sRef)).
		Str("endpoint", "api").
		Msg("primary EPG endpoint failed, trying fallback")

	// Fallback: /web/epgservice
	fallbackURL := fmt.Sprintf("/web/epgservice?sRef=%s&time=-1",
		url.QueryEscape(sRef))

	events, fallbackErr := c.fetchEPGFromURL(ctx, fallbackURL)
	if fallbackErr != nil {
		return nil, fmt.Errorf("both EPG endpoints failed - api: %w, web: %v", err, fallbackErr)
	}

	return events, nil
}

// GetBouquetEPG fetches EPG events for an entire bouquet
func (c *Client) GetBouquetEPG(ctx context.Context, bouquetRef string, days int) ([]EPGEvent, error) {
	if bouquetRef == "" {
		return nil, fmt.Errorf("bouquet reference cannot be empty")
	}
	if days < 1 || days > 14 {
		return nil, fmt.Errorf("invalid EPG days: %d (must be 1-14)", days)
	}

	// Use bouquet EPG endpoint
	epgURL := fmt.Sprintf("/api/epgbouquet?bRef=%s", url.QueryEscape(bouquetRef))

	events, err := c.fetchEPGFromURL(ctx, epgURL)
	if err != nil {
		return nil, fmt.Errorf("bouquet EPG request failed: %w", err)
	}

	return events, nil
}

// needsLatin1Conversion checks if data needs to be converted from Latin-1/ISO-8859-1 to UTF-8
func needsLatin1Conversion(data []byte, contentType string) bool {
	// If Content-Type explicitly mentions UTF-8, don't convert
	if strings.Contains(strings.ToLower(contentType), "utf-8") {
		return false
	}

	// If Content-Type explicitly mentions ISO-8859-1 or Latin-1, convert
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "iso-8859-1") || strings.Contains(ct, "latin1") {
		return true
	}

	// Heuristic: Check for invalid UTF-8 sequences that look like Latin-1
	// Look for byte patterns like 0xF6 (ö in Latin-1) that would be invalid in UTF-8
	for _, b := range data {
		// Bytes 0x80-0xFF in Latin-1 are single-byte characters
		// But in UTF-8, they must be part of multi-byte sequences
		if b >= 0x80 {
			// Check if this is a valid UTF-8 continuation
			if !utf8.Valid(data) {
				return true
			}
			break
		}
	}

	return false
}

// convertLatin1ToUTF8 converts Latin-1/ISO-8859-1 encoded bytes to UTF-8
func convertLatin1ToUTF8(latin1 []byte) []byte {
	// Allocate buffer with enough space (worst case: every byte becomes 2 bytes in UTF-8)
	buf := make([]byte, 0, len(latin1)*2)

	for _, b := range latin1 {
		if b < 0x80 {
			// ASCII range, copy directly
			buf = append(buf, b)
		} else {
			// Latin-1 byte 0x80-0xFF maps to Unicode U+0080-U+00FF
			// In UTF-8, these are encoded as two bytes: 110xxxxx 10xxxxxx
			buf = append(buf, 0xC0|(b>>6), 0x80|(b&0x3F))
		}
	}

	return buf
}

func (c *Client) fetchEPGFromURL(ctx context.Context, urlPath string) ([]EPGEvent, error) {
	decorate := func(zc *zerolog.Context) {
		zc.Str("path", urlPath)
	}

	body, err := c.get(ctx, urlPath, "epg", decorate)
	if err != nil {
		return nil, fmt.Errorf("EPG request failed: %w", err)
	}

	// Check if response starts with JSON or XML
	trimmed := bytes.TrimLeft(body, " \t\n\r")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	// If it starts with '<', it's XML (web endpoint)
	if trimmed[0] == '<' {
		return c.parseEPGXML(body)
	}

	// Otherwise try JSON (api endpoint)
	var epgResp EPGResponse
	if err := json.Unmarshal(body, &epgResp); err != nil {
		return nil, fmt.Errorf("parsing EPG response: %w", err)
	}

	// Note: Some OpenWebIF endpoints don't return a "result" field at all
	// so we check for events directly rather than validating Result

	// Filter out invalid events
	validEvents := make([]EPGEvent, 0, len(epgResp.Events))
	for _, event := range epgResp.Events {
		if event.Title != "" && event.Begin > 0 {
			validEvents = append(validEvents, event)
		}
	}

	return validEvents, nil
}

// XML structures for /web/epgservice response
type xmlEPGResponse struct {
	XMLName xml.Name      `xml:"e2eventlist"`
	Events  []xmlEPGEvent `xml:"e2event"`
}

type xmlEPGEvent struct {
	ID          int    `xml:"e2eventid"`
	Title       string `xml:"e2eventtitle"`
	Description string `xml:"e2eventdescription"`
	Start       int64  `xml:"e2eventstart"`
	Duration    int    `xml:"e2eventduration"`
	ServiceRef  string `xml:"e2eventservicereference"`
	Genre       string `xml:"e2eventgenre"`
}

func (c *Client) parseEPGXML(body []byte) ([]EPGEvent, error) {
	var xmlResp xmlEPGResponse
	if err := xml.Unmarshal(body, &xmlResp); err != nil {
		return nil, fmt.Errorf("parsing XML EPG response: %w", err)
	}

	// Convert XML events to EPGEvent format
	events := make([]EPGEvent, 0, len(xmlResp.Events))
	for _, xmlEvent := range xmlResp.Events {
		if xmlEvent.Title != "" && xmlEvent.Start > 0 {
			event := EPGEvent{
				ID:          xmlEvent.ID,
				Title:       xmlEvent.Title,
				Description: xmlEvent.Description,
				Begin:       xmlEvent.Start,
				Duration:    int64(xmlEvent.Duration),
				SRef:        xmlEvent.ServiceRef,
			}
			events = append(events, event)
		}
	}

	return events, nil
}
