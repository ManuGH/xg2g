// SPDX-License-Identifier: MIT
package openwebif

import (
	"context"
	"encoding/json"
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
}

type Options struct {
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	MaxRetries            int
	Backoff               time.Duration
	MaxBackoff            time.Duration
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
	resp, err := c.get(ctx, path, "bouquets", nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp.Body)

	var payload struct {
		Bouquets [][]string `json:"bouquets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
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

func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	maskedRef := maskValue(bouquetRef)
	decorate := func(zc *zerolog.Context) {
		zc.Str("bouquet_ref", maskedRef)
	}
	try := func(urlPath, operation string) ([][2]string, error) {
		resp, err := c.get(ctx, urlPath, operation, decorate)
		if err != nil {
			return nil, err
		}
		defer closeBody(resp.Body)

		var p svcPayload
		if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
			c.loggerFor(ctx).Error().Err(err).
				Str("event", "openwebif.decode").
				Str("operation", operation).
				Str("bouquet_ref", maskedRef).
				Msg("failed to decode services response")
			return nil, err
		}
		out := make([][2]string, 0, len(p.Services))
		for _, s := range p.Services {
			// 1:7:* = Bouquet-Container; 1:0:* = TV/Radio Services
			if strings.HasPrefix(s.ServiceRef, "1:7:") {
				continue
			}
			out = append(out, [2]string{s.ServiceName, s.ServiceRef})
		}
		return out, nil
	}

	if out, err := try("/api/getallservices?bRef="+url.QueryEscape(bouquetRef), "services.flat"); err == nil && len(out) > 0 {
		c.loggerFor(ctx).Info().Str("event", "openwebif.services").Str("bouquet_ref", maskedRef).Int("count", len(out)).Msg("fetched services via flat endpoint")
		return out, nil
	}
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

	host := parsed.Host
	if host == "" && parsed.Path != "" {
		host = parsed.Path
		parsed.Path = ""
	}
	if host == "" {
		return "", fmt.Errorf("openwebif base URL %q missing host", base)
	}

	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}

	if _, _, err := net.SplitHostPort(host); err != nil && c.port > 0 {
		host = net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(c.port))
	}

	basePath := strings.TrimSuffix(parsed.Path, "/")
	if basePath == "" {
		basePath = ""
	}

	path := "/web/stream.m3u"
	if basePath != "" {
		if !strings.HasPrefix(basePath, "/") {
			basePath = "/" + basePath
		}
		path = basePath + "/web/stream.m3u"
	}

	u := &url.URL{
		Scheme: parsed.Scheme,
		Host:   host,
		Path:   path,
	}

	q := url.Values{}
	q.Set("ref", ref)
	q.Set("device", "etc")
	if name != "" {
		q.Set("name", name)
		q.Set("fname", name)
	}
	u.RawQuery = q.Encode()
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

func (c *Client) get(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) (*http.Response, error) {
	maxAttempts := c.maxRetries + 1
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var res *http.Response
		var err error
		var status int
		var duration time.Duration

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

			start := time.Now()
			res, err = c.http.Do(req)
			duration = time.Since(start)
			if res != nil {
				status = res.StatusCode
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
			return res, nil
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
