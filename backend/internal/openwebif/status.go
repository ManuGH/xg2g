package openwebif

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// About fetches receiver metadata from /api/about (best-effort).
func (c *Client) About(ctx context.Context) (*AboutInfo, error) {
	body, err := c.get(ctx, "/api/about", "about", nil)
	if err != nil {
		return nil, err
	}
	var info AboutInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse about response: %w", err)
	}
	return &info, nil
}

// GetStatusInfo fetches current receiver status (recording, standby, service).
func (c *Client) GetStatusInfo(ctx context.Context) (*StatusInfo, error) {
	// Short cache TTL (1s) to avoid hammering but allow rapid UI updates
	const cacheKey = "statusinfo"
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			if result, ok := cached.(*StatusInfo); ok {
				return result, nil
			}
		}
	}

	body, err := c.get(ctx, "/api/statusinfo", "status.info", nil)
	if err != nil {
		return nil, err
	}

	var info StatusInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode status info: %w", err)
	}

	if c.cache != nil {
		c.cache.Set(cacheKey, &info, 2*time.Second) // 2s
	}

	return &info, nil
}

// GetCurrent fetches detailed current service information (PIDs, etc).
func (c *Client) GetCurrent(ctx context.Context) (*CurrentInfo, error) {
	// Status data changes rapidly, so very short TTL or no cache
	// We want fresh data for readiness checks.
	body, err := c.get(ctx, "/api/getcurrent", "get.current", nil)
	if err != nil {
		return nil, err
	}

	var info CurrentInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode current info: %w", err)
	}
	return &info, nil
}

// GetSignal fetches signal statistics (SNR, BER, etc).
func (c *Client) GetSignal(ctx context.Context) (*SignalInfo, error) {
	body, err := c.get(ctx, "/api/signal", "get.signal", nil)
	if err != nil {
		return nil, err
	}

	var info SignalInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to decode signal info: %w", err)
	}
	return &info, nil
}
