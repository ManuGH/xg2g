package openwebif

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/rs/zerolog"
)

type Client struct {
	base string
	port int
	http *http.Client
	log  zerolog.Logger
	host string
}

// ClientInterface defines the subset used by other packages and tests.
type ClientInterface interface {
	Bouquets(ctx context.Context) (map[string]string, error)
	Services(ctx context.Context, bouquetRef string) ([][2]string, error)
	StreamURL(ref string) string
}

func New(base string) *Client {
	// Default-Streamport
	port := 8001
	if v := os.Getenv("XG2G_STREAM_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			port = p
		}
	}
	host := extractHost(base)
	logger := xglog.WithComponent("openwebif").With().Str("host", host).Logger()
	return &Client{
		base: strings.TrimRight(base, "/"),
		port: port,
		http: &http.Client{Timeout: 30 * time.Second},
		log:  logger,
		host: host,
	}
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

func (c *Client) StreamURL(ref string) string {
	return fmt.Sprintf("%s:%d/%s", c.base, c.port, url.PathEscape(ref))
}

// Beibehaltener Helfer (nutzt jetzt New, damit ENV wirkt)
func StreamURL(base, ref string) string {
	return New(base).StreamURL(ref)
}

func (c *Client) get(ctx context.Context, path, operation string, decorate func(*zerolog.Context)) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		c.loggerFor(ctx).Error().Err(err).
			Str("event", "openwebif.request.build").
			Str("operation", operation).
			Msg("failed to build OpenWebIF request")
		return nil, err
	}
	start := time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		c.logRequest(ctx, operation, path, 0, time.Since(start), err, decorate)
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		err = fmt.Errorf("%s: %s", operation, res.Status)
		c.logRequest(ctx, operation, path, res.StatusCode, time.Since(start), err, decorate)
		closeBody(res.Body)
		return nil, err
	}
	c.logRequest(ctx, operation, path, res.StatusCode, time.Since(start), nil, decorate)
	return res, nil
}

func (c *Client) logRequest(ctx context.Context, operation, path string, status int, duration time.Duration, err error, decorate func(*zerolog.Context)) {
	endpoint := path
	if idx := strings.Index(endpoint, "?"); idx >= 0 {
		endpoint = endpoint[:idx]
	}
	builder := c.loggerFor(ctx).With().
		Str("event", "openwebif.request").
		Str("operation", operation).
		Str("method", http.MethodGet).
		Str("endpoint", endpoint).
		Int64("duration_ms", duration.Milliseconds())
	if status > 0 {
		builder = builder.Int("status", status)
	}
	if decorate != nil {
		decorate(&builder)
	}
	logger := builder.Logger()
	if err != nil {
		logger.Error().Err(err).Msg("OpenWebIF request failed")
		return
	}
	if status >= 400 {
		logger.Warn().Msg("OpenWebIF returned non-success status")
		return
	}
	logger.Info().Msg("OpenWebIF request completed")
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
