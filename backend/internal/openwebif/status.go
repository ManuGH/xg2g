package openwebif

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const (
	defaultAboutCacheTTL  = 15 * time.Second
	defaultStatusCacheTTL = 3 * time.Second
)

// InvalidateAboutCache clears the cached about metadata.
func (c *Client) InvalidateAboutCache() {
	c.aboutCacheMu.Lock()
	c.aboutCache = nil
	c.aboutCacheAt = time.Time{}
	c.aboutCacheMu.Unlock()
}

// InvalidateStatusCache clears the cached statusinfo metadata.
func (c *Client) InvalidateStatusCache() {
	c.statusCacheMu.Lock()
	c.statusCache = nil
	c.statusCacheAt = time.Time{}
	c.statusCacheMu.Unlock()
}

// About fetches receiver metadata from /api/about with 15s caching,
// singleflight request deduplication, and last-known-good fallback.
func (c *Client) About(ctx context.Context) (*AboutInfo, error) {
	c.aboutCacheMu.RLock()
	if c.aboutCache != nil && time.Since(c.aboutCacheAt) < defaultAboutCacheTTL {
		cached := *c.aboutCache
		c.aboutCacheMu.RUnlock()
		return &cached, nil
	}
	c.aboutCacheMu.RUnlock()

	val, err, _ := c.aboutGroup.Do("about.info", func() (any, error) {
		c.aboutCacheMu.RLock()
		if c.aboutCache != nil && time.Since(c.aboutCacheAt) < defaultAboutCacheTTL {
			cached := *c.aboutCache
			c.aboutCacheMu.RUnlock()
			return &cached, nil
		}
		c.aboutCacheMu.RUnlock()

		// Detach from the leader's request context: this singleflight result is shared
		// by every joined waiter, so one caller disconnecting must not cancel the
		// upstream call for all of them. Use an independent, bounded timeout instead.
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		body, err := c.get(reqCtx, "/api/about", "about", nil)
		if err != nil {
			return nil, err
		}

		var info AboutInfo
		if err := json.Unmarshal(body, &info); err != nil {
			return nil, fmt.Errorf("parse about response: %w", err)
		}

		c.aboutCacheMu.Lock()
		c.aboutCache = &info
		c.aboutCacheAt = time.Now()
		c.aboutCacheMu.Unlock()

		return &info, nil
	})

	if err != nil {
		c.aboutCacheMu.RLock()
		if c.aboutCache != nil {
			cached := *c.aboutCache
			c.aboutCacheMu.RUnlock()
			c.loggerFor(ctx).Warn().Err(err).Msg("OpenWebIF about fetch failed or timed out; serving Last-Known-Good cached about info")
			return &cached, nil
		}
		c.aboutCacheMu.RUnlock()
		return nil, err
	}

	info := val.(*AboutInfo)
	copied := *info
	return &copied, nil
}

// GetStatusInfo fetches current receiver status (recording, standby, service)
// with 3s caching, singleflight request deduplication, and last-known-good fallback.
func (c *Client) GetStatusInfo(ctx context.Context) (*StatusInfo, error) {
	c.statusCacheMu.RLock()
	if c.statusCache != nil && time.Since(c.statusCacheAt) < defaultStatusCacheTTL {
		cached := *c.statusCache
		c.statusCacheMu.RUnlock()
		return &cached, nil
	}
	c.statusCacheMu.RUnlock()

	val, err, _ := c.statusGroup.Do("status.info", func() (any, error) {
		c.statusCacheMu.RLock()
		if c.statusCache != nil && time.Since(c.statusCacheAt) < defaultStatusCacheTTL {
			cached := *c.statusCache
			c.statusCacheMu.RUnlock()
			return &cached, nil
		}
		c.statusCacheMu.RUnlock()

		// Detach from the leader's request context: this singleflight result is shared
		// by every joined waiter, so one caller disconnecting must not cancel the
		// upstream call for all of them. Use an independent, bounded timeout instead.
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()

		body, err := c.get(reqCtx, "/api/statusinfo", "status.info", nil)
		if err != nil {
			return nil, err
		}

		var info StatusInfo
		if err := json.Unmarshal(body, &info); err != nil {
			return nil, fmt.Errorf("failed to decode status info: %w", err)
		}

		c.statusCacheMu.Lock()
		c.statusCache = &info
		c.statusCacheAt = time.Now()
		c.statusCacheMu.Unlock()

		return &info, nil
	})

	if err != nil {
		c.statusCacheMu.RLock()
		if c.statusCache != nil {
			cached := *c.statusCache
			c.statusCacheMu.RUnlock()
			c.loggerFor(ctx).Warn().Err(err).Msg("OpenWebIF status fetch failed or timed out; serving Last-Known-Good cached status info")
			return &cached, nil
		}
		c.statusCacheMu.RUnlock()
		return nil, err
	}

	info := val.(*StatusInfo)
	copied := *info
	return &copied, nil
}

// GetCurrent fetches detailed current service information (PIDs, etc) with singleflight deduplication.
func (c *Client) GetCurrent(ctx context.Context) (*CurrentInfo, error) {
	val, err, _ := c.currentGroup.Do("get.current", func() (any, error) {
		// Detach from the leader's request context: this singleflight result is shared
		// by every joined waiter, so one caller disconnecting must not cancel the
		// upstream call for all of them. Use an independent, bounded timeout instead.
		reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		body, err := c.get(reqCtx, "/api/getcurrent", "get.current", nil)
		if err != nil {
			return nil, err
		}

		var info CurrentInfo
		if err := json.Unmarshal(body, &info); err != nil {
			return nil, fmt.Errorf("failed to decode current info: %w", err)
		}
		return &info, nil
	})

	if err != nil {
		return nil, err
	}
	info := val.(*CurrentInfo)
	return info, nil
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
