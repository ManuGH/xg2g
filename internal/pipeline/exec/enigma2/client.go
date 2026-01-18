// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package enigma2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
)

// Client interacts with the Enigma2/OpenWebIf API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	limiter    *rate.Limiter
	maxRetries int
	backoff    time.Duration
	// streamPort was renamed to StreamPort
	maxBackoff time.Duration
	Username   string
	Password   string
	userAgent  string
	rnd        *rand.Rand
	mu         sync.Mutex
	// This bypasses optional middleware issues and ensures predictable stream sourcening.
	UseWebIFStreams bool
	// StreamPort is the port for direct stream URLs (e.g. 8001 for Enigma2, 17999 for optional middleware).
	// When set, ResolveStreamURL will build direct URLs instead of querying /web/stream.m3u.
	StreamPort int
}

// Options configures the Enigma2 client behavior.
type Options struct {
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
	MaxRetries            int
	Backoff               time.Duration
	MaxBackoff            time.Duration
	Username              string
	Password              string
	UserAgent             string
	RateLimit             rate.Limit
	RateLimitBurst        int
	UseWebIFStreams       bool
	StreamPort            int // Port for direct stream URLs (0 = use /web/stream.m3u to let receiver decide)
}

const (
	defaultTimeout        = 5 * time.Second
	defaultRetries        = 2
	defaultBackoff        = 200 * time.Millisecond
	defaultMaxBackoff     = 2 * time.Second
	defaultRateLimit      = 10
	defaultRateLimitBurst = 20
)

// NewClient creates a new Enigma2 client.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return NewClientWithOptions(baseURL, Options{Timeout: timeout})
}

// NewClientWithOptions creates a new Enigma2 client with explicit options.
func NewClientWithOptions(baseURL string, opts Options) *Client {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u, err := url.Parse(trimmed); err == nil {
		if u.User != nil && opts.Username == "" {
			opts.Username = u.User.Username()
			if pass, ok := u.User.Password(); ok {
				opts.Password = pass
			}
		}
		u.User = nil
		trimmed = strings.TrimRight(u.String(), "/")
	}

	nopts := normalizeOptions(opts)
	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: nopts.ResponseHeaderTimeout,
		TLSHandshakeTimeout:   5 * time.Second,
	}

	return &Client{
		BaseURL: trimmed,
		HTTPClient: &http.Client{
			Timeout:   nopts.Timeout,
			Transport: transport,
		},
		limiter:         rate.NewLimiter(nopts.RateLimit, nopts.RateLimitBurst),
		maxRetries:      nopts.MaxRetries,
		backoff:         nopts.Backoff,
		maxBackoff:      nopts.MaxBackoff,
		Username:        nopts.Username,
		Password:        nopts.Password,
		userAgent:       nopts.UserAgent,
		rnd:             rand.New(rand.NewSource(time.Now().UnixNano())), // #nosec G404 -- jitter only
		UseWebIFStreams: opts.UseWebIFStreams,
		StreamPort:      opts.StreamPort,
	}
}

func normalizeOptions(opts Options) Options {
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.ResponseHeaderTimeout <= 0 {
		opts.ResponseHeaderTimeout = defaultTimeout
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = defaultRetries
	}
	if opts.Backoff <= 0 {
		opts.Backoff = defaultBackoff
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = defaultMaxBackoff
	}
	if opts.RateLimit <= 0 {
		opts.RateLimit = rate.Limit(defaultRateLimit)
	}
	if opts.RateLimitBurst <= 0 {
		opts.RateLimitBurst = defaultRateLimitBurst
	}
	if strings.TrimSpace(opts.UserAgent) == "" {
		opts.UserAgent = "xg2g-v3"
	}
	return opts
}

// Zap requests the receiver to switch to the specified service reference.
func (c *Client) Zap(ctx context.Context, sref string) error {
	params := url.Values{}
	// NOTE: OpenWebIf API (Zap) specifically requires "sRef". "ref" causes a "parameter missing" error.
	params.Set("sRef", strings.ToUpper(sref))

	var res Response
	if err := c.get(ctx, "/api/zap", params, &res); err != nil {
		return err
	}

	if !res.Result {
		return fmt.Errorf("zap failed: %s", res.Message)
	}
	return nil
}

// GetCurrent retrieves typical current service information.
func (c *Client) GetCurrent(ctx context.Context) (*CurrentInfo, error) {
	var res CurrentInfo
	if err := c.get(ctx, "/api/getcurrent", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetSignal retrieves tuner signal stats (SNR, AGC, BER, Lock).
func (c *Client) GetSignal(ctx context.Context) (*Signal, error) {
	var res Signal
	// /api/signal usually returns directly
	if err := c.get(ctx, "/api/signal", nil, &res); err != nil {
		return nil, err
	}

	// Fallback: Some receivers (e.g. older OpenWebIF or specific images) do not return the "lock" field in JSON.
	// If Locked is false but SNR is high (e.g. > 50%), assume signal is locked.
	if !res.Locked && res.Snr > 50 {
		res.Locked = true
	}

	return &res, nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, v interface{}) error {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	u.Path = path
	u.RawQuery = params.Encode()

	resp, err := c.doGet(ctx, u.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: api returned status %d", ErrUpstreamUnavailable, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode error: %w", err)
	}
	return nil
}

func (c *Client) doGet(ctx context.Context, rawURL string) (*http.Response, error) {
	tracer := telemetry.Tracer("xg2g.enigma2")
	route, urlLabel := traceLabels(rawURL)
	ctx, span := tracer.Start(ctx, "xg2g.v3.enigma2.request", trace.WithSpanKind(trace.SpanKindClient))
	span.SetAttributes(
		attribute.String("http.method", http.MethodGet),
		attribute.String("http.route", route),
		attribute.String("http.url", urlLabel),
	)
	defer span.End()

	maxAttempts := c.maxRetries + 1
	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, attemptSpan := tracer.Start(ctx, "xg2g.v3.enigma2.request.attempt", trace.WithSpanKind(trace.SpanKindClient))
		attemptSpan.SetAttributes(
			attribute.Int("attempt", attempt),
			attribute.Bool("retry", attempt > 1),
		)

		if c.limiter != nil {
			if err := c.limiter.Wait(attemptCtx); err != nil {
				attemptSpan.RecordError(err)
				attemptSpan.SetStatus(codes.Error, err.Error())
				attemptSpan.End()
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return nil, err
			}
		}

		req, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			attemptSpan.RecordError(err)
			attemptSpan.SetStatus(codes.Error, err.Error())
			attemptSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		c.applyHeaders(req)
		otel.GetTextMapPropagator().Inject(attemptCtx, propagation.HeaderCarrier(req.Header))

		start := time.Now()
		resp, err := c.HTTPClient.Do(req)
		duration := time.Since(start)

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}

		retry := (err != nil || status != http.StatusOK) && attempt < maxAttempts && shouldRetry(resp, err)
		recordAttemptMetrics(http.MethodGet, route, status, duration, err, retry)

		attemptSpan.SetAttributes(telemetry.HTTPAttributes(http.MethodGet, route, urlLabel, status)...)
		if err != nil {
			attemptSpan.RecordError(err)
		}
		if err != nil || status >= http.StatusBadRequest {
			statusText := http.StatusText(status)
			if statusText == "" {
				statusText = "request failed"
			}
			attemptSpan.SetStatus(codes.Error, statusText)
		} else {
			attemptSpan.SetStatus(codes.Ok, "")
		}
		attemptSpan.End()

		if err == nil && status < http.StatusInternalServerError {
			span.SetAttributes(telemetry.HTTPAttributes(http.MethodGet, route, urlLabel, status)...)
			if status >= http.StatusBadRequest {
				span.SetStatus(codes.Error, http.StatusText(status))
			} else {
				span.SetStatus(codes.Ok, "")
			}
			return resp, nil
		}

		if resp != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		lastErr = err
		lastStatus = status

		if !retry {
			break
		}

		wait := c.backoffFor(attempt - 1)
		if err := sleepWithContext(ctx, wait); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
	}

	if lastStatus > 0 {
		span.SetAttributes(telemetry.HTTPAttributes(http.MethodGet, route, urlLabel, lastStatus)...)
		if lastStatus >= http.StatusBadRequest {
			span.SetStatus(codes.Error, http.StatusText(lastStatus))
		}
	}
	if lastErr != nil {
		span.RecordError(lastErr)
		span.SetStatus(codes.Error, lastErr.Error())
		return nil, lastErr
	}
	return nil, fmt.Errorf("request failed")
}

func (c *Client) applyHeaders(req *http.Request) {
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "application/json")
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
}

func shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	if resp == nil {
		return true
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return true
	}
	return false
}

func (c *Client) backoffFor(attempt int) time.Duration {
	wait := c.backoff * time.Duration(1<<attempt)
	if wait > c.maxBackoff {
		wait = c.maxBackoff
	}
	jitter := time.Duration(c.randInt63n(int64(wait/5 + 1)))
	return wait + jitter
}

func (c *Client) randInt63n(n int64) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rnd.Int63n(n)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func traceLabels(rawURL string) (string, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, rawURL
	}
	route := u.Path
	if route == "" {
		route = "/"
	}
	urlLabel := route
	if u.RawQuery != "" {
		urlLabel += "?"
	}
	return route, urlLabel
}
