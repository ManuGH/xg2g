package diagnostics

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// DVRChecker implements HealthChecker for the DVR subsystem.
type DVRChecker struct {
	receiverURL string
	httpClient  *http.Client
	cache       *LKGCache
}

// NewDVRChecker creates a new DVR health checker.
func NewDVRChecker(receiverURL string, cache *LKGCache) *DVRChecker {
	return &DVRChecker{
		receiverURL: receiverURL,
		httpClient: &http.Client{
			Timeout: 3 * time.Second, // Per ADR-SRE-002 P0-D: 3s max for DVR
		},
		cache: cache,
	}
}

// MovieListResponse represents OpenWebIF movielist response structure.
type MovieListResponse struct {
	Result bool `json:"result"`
	// Additional fields can be added as needed
}

// Check probes the receiver's movielist endpoint.
// Per ADR-SRE-002:
//   - ok: result=true with valid data
//   - degraded: result=true but partial read errors (future enhancement)
//   - unavailable: result=false, timeout, or receiver unavailable
func (d *DVRChecker) Check(ctx context.Context) SubsystemHealth {
	health := SubsystemHealth{
		Subsystem:   SubsystemDVR,
		MeasuredAt:  time.Now(),
		Source:      SourceProbe,
		Criticality: Optional, // Per ADR-SRE-002 P0-A
	}

	req, err := http.NewRequestWithContext(ctx, "GET", d.receiverURL+"/api/movielist", nil)
	if err != nil {
		return d.buildUnavailableResponse(health, ErrUpstreamTimeout)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return d.buildUnavailableResponse(health, ErrUpstreamTimeout)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return d.buildUnavailableResponse(health, ErrReceiverHTTPError)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return d.buildUnavailableResponse(health, ErrUpstreamParseError)
	}

	var movieList MovieListResponse
	if err := json.Unmarshal(body, &movieList); err != nil {
		return d.buildUnavailableResponse(health, ErrUpstreamParseError)
	}

	if !movieList.Result {
		// OpenWebIF returned result=false
		return d.buildUnavailableResponse(health, ErrUpstreamResultFalse)
	}

	// Success: DVR is available
	now := time.Now()
	health.Status = OK
	health.LastOK = &now

	// TODO: Parse actual recording count from response
	// For now, store dummy values in cache
	d.cache.SetDVR("primary", 0, 0)

	return health
}

// buildUnavailableResponse creates an unavailable health response with LKG cache fallback.
func (d *DVRChecker) buildUnavailableResponse(health SubsystemHealth, errorCode string) SubsystemHealth {
	health.Status = Unavailable
	health.ErrorCode = errorCode
	health.ErrorMessage = ErrorMessages[errorCode]

	// Check for cached data (Last-Known-Good)
	if cached := d.cache.GetDVR("primary"); cached != nil {
		health.LastOK = &cached.LastOK
		health.Details = DVRDetails{
			CachedRecordingCount: cached.RecordingCount,
			CachedTimerCount:     cached.TimerCount,
			CacheAgeSeconds:      int64(time.Since(cached.LastOK).Seconds()),
		}
	}

	return health
}
