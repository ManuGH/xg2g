// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Package provider implements metadata fetchers for external providers.
// TVMaze data is licensed under CC BY-SA 4.0 (https://creativecommons.org/licenses/by-sa/4.0/).
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/epg/matcher"
	"github.com/ManuGH/xg2g/internal/resilience"
	"golang.org/x/time/rate"
)

const (
	DefaultTVMazeBaseURL   = "https://api.tvmaze.com"
	DefaultTVMazeUserAgent = "xg2g/3.x (+https://github.com/ManuGH/xg2g)"
	DefaultTVMazeRPS       = 2.0 // Conservative outbound rate limit to protect public endpoints
	DefaultTVMazeBurst     = 4
	DefaultTVMazeTimeout   = 5 * time.Second
)

// TVMazeConfig holds configuration parameters for the TVMaze API client.
type TVMazeConfig struct {
	BaseURL           string
	UserAgent         string
	RequestsPerSecond float64
	Burst             int
	Timeout           time.Duration
	HTTPClient        *http.Client
}

// DefaultTVMazeConfig provides safe default settings for TVMaze communication.
func DefaultTVMazeConfig() TVMazeConfig {
	return TVMazeConfig{
		BaseURL:           DefaultTVMazeBaseURL,
		UserAgent:         DefaultTVMazeUserAgent,
		RequestsPerSecond: DefaultTVMazeRPS,
		Burst:             DefaultTVMazeBurst,
		Timeout:           DefaultTVMazeTimeout,
	}
}

// TVMazeClient implements epg.MetadataProvider for the TVMaze public REST API.
type TVMazeClient struct {
	cfg            TVMazeConfig
	httpClient     *http.Client
	rateLimiter    *rate.Limiter
	circuitBreaker *resilience.CircuitBreaker

	mu           sync.RWMutex
	backoffUntil time.Time
}

// NewTVMazeClient creates a resilient TVMaze API client.
func NewTVMazeClient(cfg TVMazeConfig) *TVMazeClient {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultTVMazeBaseURL
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultTVMazeUserAgent
	}
	if cfg.RequestsPerSecond <= 0 {
		cfg.RequestsPerSecond = DefaultTVMazeRPS
	}
	if cfg.Burst <= 0 {
		cfg.Burst = DefaultTVMazeBurst
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTVMazeTimeout
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	// 3 failures in 60s window opens breaker; 30s reset timeout
	cb := resilience.NewCircuitBreaker("tvmaze_provider", 3, 3, 60*time.Second, 30*time.Second)

	return &TVMazeClient{
		cfg:            cfg,
		httpClient:     httpClient,
		rateLimiter:    rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), cfg.Burst),
		circuitBreaker: cb,
	}
}

func (c *TVMazeClient) Name() string {
	return "tvmaze"
}

func (c *TVMazeClient) Lookup(ctx context.Context, fp epg.ProgrammeFingerprint) (*epg.EnrichmentData, error) {
	if fp.NormalizedTitle == "" {
		return &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusNoMatch,
			FetchedAt:          time.Now(),
		}, nil
	}

	// 1. Check adaptive backoff (HTTP 429 Retry-After)
	c.mu.RLock()
	if !c.backoffUntil.IsZero() && time.Now().Before(c.backoffUntil) {
		c.mu.RUnlock()
		return &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusTransientFailure,
		}, fmt.Errorf("tvmaze: rate limited, backing off until %s", c.backoffUntil)
	}
	c.mu.RUnlock()

	// 2. Wait for rate limiter permission
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("tvmaze rate limiter wait: %w", err)
	}

	var results []matcher.TVMazeSearchResult
	searchURL := fmt.Sprintf("%s/search/shows?q=%s", c.cfg.BaseURL, url.QueryEscape(fp.NormalizedTitle))

	// 3. Execute search call within circuit breaker
	err := c.circuitBreaker.Execute(func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.cfg.UserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			c.circuitBreaker.RecordTechnicalFailure()
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			c.circuitBreaker.RecordTechnicalFailure()
			retryAfterSec := parseRetryAfter(resp.Header.Get("Retry-After"))
			c.mu.Lock()
			c.backoffUntil = time.Now().Add(retryAfterSec)
			c.mu.Unlock()
			return fmt.Errorf("tvmaze returned 429 Too Many Requests (retry after %s)", retryAfterSec)
		}

		if resp.StatusCode >= 500 {
			c.circuitBreaker.RecordTechnicalFailure()
			return fmt.Errorf("tvmaze server error: %d", resp.StatusCode)
		}

		if resp.StatusCode == http.StatusNotFound {
			results = nil
			c.circuitBreaker.RecordSuccess()
			return nil
		}

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("tvmaze unexpected status: %d", resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
			return fmt.Errorf("tvmaze json decode error: %w", err)
		}

		c.circuitBreaker.RecordSuccess()
		return nil
	})

	if err != nil {
		return &epg.EnrichmentData{
			FingerprintKey:     fp.Key(),
			FingerprintVersion: fp.FingerprintVersion,
			MatcherVersion:     epg.CurrentMatcherVersion,
			Status:             epg.MatchStatusTransientFailure,
		}, err
	}

	// 4. Deterministic candidate matching
	matchedShow, class := matcher.MatchTVMazeResults(fp, results)
	if class == matcher.MatchNone || matchedShow == nil {
		// Deterministic negative match
		return matcher.BuildEnrichmentFromShow(fp, nil, nil, time.Now()), nil
	}

	// 5. Episode refinement (if season and episode are present)
	var matchedEpisode *matcher.TVMazeEpisode
	if fp.Season > 0 && fp.Episode > 0 {
		matchedEpisode = c.fetchEpisode(ctx, matchedShow.ID, fp.Season, fp.Episode)
	}

	return matcher.BuildEnrichmentFromShow(fp, matchedShow, matchedEpisode, time.Now()), nil
}

func (c *TVMazeClient) fetchEpisode(ctx context.Context, showID, season, episode int) *matcher.TVMazeEpisode {
	epURL := fmt.Sprintf("%s/shows/%d/episodebynumber?season=%d&number=%d", c.cfg.BaseURL, showID, season, episode)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, epURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var ep matcher.TVMazeEpisode
	if err := json.NewDecoder(resp.Body).Decode(&ep); err != nil {
		return nil
	}

	return &ep
}

func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 10 * time.Second // Conservative fallback
	}
	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		diff := time.Until(t)
		if diff > 0 {
			return diff
		}
	}
	return 10 * time.Second
}
