// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/stream/ingest/admission"
)

// Client handles secure media requests to the private upstream relay on the receiver.
type Client struct {
	cfg        ClientConfig
	httpClient *http.Client
	selfHosts  map[string]struct{}
	relayURL   *url.URL
}

// NewClient creates a new private relay client with loopback protection and authentication.
func NewClient(cfg ClientConfig) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.RelayBaseURL)
	if baseURL == "" || strings.TrimSpace(cfg.AuthToken) == "" {
		return nil, ErrInvalidRelayConfig
	}

	parsedRelay, err := url.Parse(baseURL)
	if err != nil || parsedRelay.Host == "" || parsedRelay.User != nil || parsedRelay.Fragment != "" ||
		(strings.ToLower(parsedRelay.Scheme) != "http" && strings.ToLower(parsedRelay.Scheme) != "https") {
		return nil, fmt.Errorf("%w: invalid relay base URL", ErrInvalidRelayConfig)
	}

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	responseHeaderTimeout := cfg.ResponseHeaderTimeout
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = 15 * time.Second
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: responseHeaderTimeout,
		DisableCompression:    true, // MPEG-TS is already compressed
	}

	selfHosts := make(map[string]struct{})
	for _, addr := range cfg.SelfAddresses {
		cleaned := normalizeHost(strings.TrimSpace(addr))
		if cleaned == "" {
			continue
		}
		selfHosts[cleaned] = struct{}{}
		if host, _, err := net.SplitHostPort(cleaned); err == nil {
			selfHosts[normalizeHost(host)] = struct{}{}
		}
	}

	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Transport: transport,
			// Live media bodies are intentionally unbounded in duration. The
			// caller's context and response-header timeout bound startup.
			Timeout: 0,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				// Never forward the relay token to another origin.
				return http.ErrUseLastResponse
			},
		},
		selfHosts: selfHosts,
		relayURL:  parsedRelay,
	}, nil
}

// IsLoopbackTarget checks whether a target URL resolves back to xg2g itself.
func (c *Client) IsLoopbackTarget(rawTarget string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawTarget))
	if err != nil {
		return false
	}

	hostname := normalizeHost(parsed.Hostname())
	if hostname == "" {
		return false
	}
	if hostname == "localhost" || strings.HasSuffix(hostname, ".localhost") {
		return true
	}
	if ip := net.ParseIP(strings.Trim(hostname, "[]")); ip != nil && ip.IsLoopback() {
		return true
	}

	if _, match := c.selfHosts[hostname]; match {
		return true
	}
	if _, match := c.selfHosts[normalizeHost(parsed.Host)]; match {
		return true
	}
	return false
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
}

type leasedStreamReader struct {
	io.ReadCloser
	lease admission.SlotLease
	once  sync.Once
	err   error
}

func (r *leasedStreamReader) Close() error {
	r.once.Do(func() {
		var closeErr error
		if r.ReadCloser != nil {
			closeErr = r.ReadCloser.Close()
		}
		if r.lease != nil {
			closeErr = errors.Join(closeErr, r.lease.Release())
		}
		r.err = closeErr
	})
	return r.err
}

// Fetch opens an authenticated stream through the private relay, requiring a valid SlotLease.
func (c *Client) Fetch(ctx context.Context, lease admission.SlotLease, opaqueSourceID, targetURL string) (io.ReadCloser, error) {
	if lease == nil || lease.SourceID() != opaqueSourceID || !lease.IsActive() {
		return nil, ErrMissingSlotLease
	}

	if c.IsLoopbackTarget(targetURL) {
		return nil, ErrRecursiveRelayTarget
	}

	targetParsed, err := url.Parse(targetURL)
	targetScheme := ""
	if targetParsed != nil {
		targetScheme = strings.ToLower(targetParsed.Scheme)
	}
	if err != nil || targetParsed.Host == "" || targetParsed.User != nil || targetParsed.Fragment != "" ||
		(targetScheme != "http" && targetScheme != "https") {
		return nil, fmt.Errorf("%w: invalid target upstream URL", ErrInvalidRelayConfig)
	}

	reqURL := *c.relayURL
	reqURL.Path = strings.TrimRight(reqURL.Path, "/") + "/stream"
	q := reqURL.Query()
	q.Set("id", opaqueSourceID)
	reqURL.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create relay request: %w", err)
	}

	httpReq.Header.Set("X-Relay-Token", c.cfg.AuthToken)
	httpReq.Header.Set("X-Target-URI", targetURL)
	httpReq.Header.Set("User-Agent", "xg2g-ingest-relay/1.0")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRelayUnavailable, err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		return &leasedStreamReader{
			ReadCloser: resp.Body,
			lease:      lease,
		}, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		_ = resp.Body.Close()
		return nil, ErrRelayUnauthorized
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		_ = resp.Body.Close()
		return nil, ErrRelayUnavailable
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%w: status %d", ErrRelayUnavailable, resp.StatusCode)
	}
}
