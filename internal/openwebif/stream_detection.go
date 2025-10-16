package openwebif

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// maskServiceRef masks sensitive parts of service reference for logging.
func maskServiceRef(ref string) string {
	if len(ref) > 20 {
		return ref[:10] + "..." + ref[len(ref)-7:]
	}
	return ref
}

// StreamInfo contains information about a tested stream endpoint.
type StreamInfo struct {
	URL          string    // The working stream URL
	Port         int       // Port used (8001, 17999, or proxy port)
	SupportsHEAD bool      // Whether the endpoint supports HEAD requests
	UseProxy     bool      // Whether to use the integrated proxy
	TestedAt     time.Time // When this was last tested
	TestError    error     // Last test error (if any)
}

// StreamDetector handles smart detection of optimal stream endpoints.
type StreamDetector struct {
	cache      map[string]*StreamInfo // Key: ServiceRef
	cacheMu    sync.RWMutex
	httpClient *http.Client
	logger     zerolog.Logger

	// Configuration
	receiverHost string // e.g., "192.168.1.100"
	proxyEnabled bool
	proxyHost    string // e.g., "192.168.1.50:18000"
	cacheTTL     time.Duration
}

// NewStreamDetector creates a new smart stream detector.
func NewStreamDetector(receiverHost string, logger zerolog.Logger) *StreamDetector {
	proxyEnabled := os.Getenv("XG2G_ENABLE_STREAM_PROXY") == "true"
	proxyHost := os.Getenv("XG2G_STREAM_BASE") // e.g., http://host:18000

	return &StreamDetector{
		cache: make(map[string]*StreamInfo),
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				DisableKeepAlives:   true,
				MaxIdleConns:        10,
				IdleConnTimeout:     5 * time.Second,
				TLSHandshakeTimeout: 2 * time.Second,
			},
		},
		logger:       logger,
		receiverHost: receiverHost,
		proxyEnabled: proxyEnabled,
		proxyHost:    proxyHost,
		cacheTTL:     24 * time.Hour, // Cache results for 24 hours
	}
}

// IsEnabled checks if smart stream detection is enabled.
func IsEnabled() bool {
	return os.Getenv("XG2G_SMART_STREAM_DETECTION") == "true"
}

// DetectStreamURL determines the optimal stream URL for a given service reference.
// It tests multiple endpoints and returns the best working option.
func (sd *StreamDetector) DetectStreamURL(ctx context.Context, serviceRef, channelName string) (*StreamInfo, error) {
	// Check cache first
	if cached := sd.getCached(serviceRef); cached != nil {
		sd.logger.Debug().
			Str("service_ref", maskServiceRef(serviceRef)).
			Str("url", cached.URL).
			Int("port", cached.Port).
			Bool("use_proxy", cached.UseProxy).
			Msg("using cached stream info")
		return cached, nil
	}

	// Test endpoints in order of preference
	candidates := sd.buildCandidates(serviceRef)

	for _, candidate := range candidates {
		if sd.testEndpoint(ctx, candidate) {
			// Found working endpoint
			info := &StreamInfo{
				URL:          candidate.URL,
				Port:         candidate.Port,
				SupportsHEAD: candidate.SupportsHEAD,
				UseProxy:     candidate.UseProxy,
				TestedAt:     time.Now(),
			}

			// Cache the result with lock
			sd.cacheMu.Lock()
			sd.cache[serviceRef] = info
			sd.cacheMu.Unlock()

			sd.logger.Info().
				Str("service_ref", maskServiceRef(serviceRef)).
				Str("channel", channelName).
				Str("url", info.URL).
				Int("port", info.Port).
				Bool("supports_head", info.SupportsHEAD).
				Bool("use_proxy", info.UseProxy).
				Msg("detected optimal stream endpoint")

			return info, nil
		}
	}

	// No working endpoint found - return best guess (port 8001)
	fallback := &StreamInfo{
		URL:       fmt.Sprintf("http://%s:8001/%s", sd.receiverHost, serviceRef),
		Port:      8001,
		TestedAt:  time.Now(),
		TestError: fmt.Errorf("no working endpoint found, using fallback"),
	}

	sd.logger.Warn().
		Str("service_ref", maskServiceRef(serviceRef)).
		Str("channel", channelName).
		Msg("no working stream endpoint detected, using fallback")

	return fallback, nil
}

// streamCandidate represents a potential stream endpoint to test.
type streamCandidate struct {
	URL          string
	Port         int
	SupportsHEAD bool
	UseProxy     bool
	Priority     int // Lower = higher priority
}

// buildCandidates creates an ordered list of stream endpoints to test.
func (sd *StreamDetector) buildCandidates(serviceRef string) []streamCandidate {
	candidates := []streamCandidate{
		// Priority 1: Direct port 8001 (most common, best performance)
		{
			URL:      fmt.Sprintf("http://%s:8001/%s", sd.receiverHost, serviceRef),
			Port:     8001,
			Priority: 1,
		},
		// Priority 2: Direct port 17999 (if it supports HEAD)
		{
			URL:      fmt.Sprintf("http://%s:17999/%s", sd.receiverHost, serviceRef),
			Port:     17999,
			Priority: 2,
		},
	}

	// Priority 3: Proxy for port 17999 (if proxy is enabled)
	if sd.proxyEnabled && sd.proxyHost != "" {
		candidates = append(candidates, streamCandidate{
			URL:      fmt.Sprintf("%s/%s", sd.proxyHost, serviceRef),
			Port:     18000, // Proxy port (extracted from proxyHost)
			UseProxy: true,
			Priority: 3,
		})
	}

	return candidates
}

// testEndpoint tests if a stream endpoint is working by sending a HEAD request.
func (sd *StreamDetector) testEndpoint(ctx context.Context, candidate streamCandidate) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, candidate.URL, nil)
	if err != nil {
		return false
	}

	resp, err := sd.httpClient.Do(req)
	if err != nil {
		sd.logger.Debug().
			Err(err).
			Str("url", candidate.URL).
			Int("port", candidate.Port).
			Msg("stream endpoint HEAD test failed")
		return false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			sd.logger.Debug().Err(err).Msg("failed to close response body")
		}
	}()

	// Accept 200 OK or 206 Partial Content (streaming)
	success := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent

	sd.logger.Debug().
		Str("url", candidate.URL).
		Int("port", candidate.Port).
		Int("status", resp.StatusCode).
		Bool("success", success).
		Msg("stream endpoint HEAD test result")

	return success
}

// getCached retrieves cached stream info if still valid.
func (sd *StreamDetector) getCached(serviceRef string) *StreamInfo {
	sd.cacheMu.RLock()
	defer sd.cacheMu.RUnlock()

	info, exists := sd.cache[serviceRef]
	if !exists {
		return nil
	}

	// Check if cache is still valid
	if time.Since(info.TestedAt) > sd.cacheTTL {
		return nil
	}

	return info
}

// DetectBatch detects optimal stream URLs for multiple services in parallel.
func (sd *StreamDetector) DetectBatch(ctx context.Context, services [][2]string) (map[string]*StreamInfo, error) {
	results := make(map[string]*StreamInfo)
	resultsMu := sync.Mutex{}

	// Use worker pool to limit concurrent requests
	const maxWorkers = 10
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, svc := range services {
		serviceRef := svc[1]
		channelName := svc[0]

		wg.Add(1)
		go func(ref, name string) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			info, err := sd.DetectStreamURL(ctx, ref, name)
			if err == nil && info != nil {
				resultsMu.Lock()
				results[ref] = info
				resultsMu.Unlock()
			}
		}(serviceRef, channelName)
	}

	wg.Wait()

	sd.logger.Info().
		Int("total", len(services)).
		Int("detected", len(results)).
		Msg("batch stream detection completed")

	return results, nil
}

// ClearCache clears all cached stream detection results.
func (sd *StreamDetector) ClearCache() {
	sd.cacheMu.Lock()
	defer sd.cacheMu.Unlock()
	sd.cache = make(map[string]*StreamInfo)
	sd.logger.Info().Msg("stream detection cache cleared")
}
