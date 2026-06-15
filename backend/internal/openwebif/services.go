package openwebif

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"net/url"
	"strings"
	"time"
)

// Bouquets retrieves all available bouquets from the receiver.
func (c *Client) Bouquets(ctx context.Context) (map[string]string, error) {
	const cacheKey = "bouquets"

	// Check cache if available
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			if result, ok := cached.(map[string]string); ok {
				c.loggerFor(ctx).Debug().Str("event", "cache.hit").Str("key", cacheKey).Msg("serving from cache")
				return result, nil
			}
		}
	}

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

	// Store in cache if available
	if c.cache != nil {
		c.cache.Set(cacheKey, out, c.cacheTTL)
		c.loggerFor(ctx).Debug().Str("event", "cache.set").Str("key", cacheKey).Dur("ttl", c.cacheTTL).Msg("cached result")
	}

	c.loggerFor(ctx).Info().Str("event", "openwebif.bouquets").Int("count", len(out)).Msg("fetched bouquets")
	return out, nil
}

// /api/bouquets: [["<ref>","<name>"], ...]

// Services retrieves all services for a given bouquet.
func (c *Client) Services(ctx context.Context, bouquetRef string) ([][2]string, error) {
	// Check cache if available
	cacheKey := "services:" + bouquetRef
	if c.cache != nil {
		if cached, ok := c.cache.Get(cacheKey); ok {
			if result, ok := cached.([][2]string); ok {
				c.loggerFor(ctx).Debug().Str("event", "cache.hit").Str("key", cacheKey).Msg("serving from cache")
				return result, nil
			}
		}
	}

	maskedRef := maskValue(bouquetRef)
	decorate := func(zc *zerolog.Context) {
		zc.Str("bouquet_ref", maskedRef)
	}
	type endpointSpec struct {
		path       string
		operation  string
		preferFlat bool
	}
	try := func(ep endpointSpec) ([][2]string, error) {
		body, err := c.get(ctx, ep.path, ep.operation, decorate)
		if err != nil {
			return nil, err
		}

		// Fast schema check: ensure "services" key exists and is not null.
		var shape struct {
			Services json.RawMessage `json:"services"`
		}
		if err := json.Unmarshal(body, &shape); err != nil {
			c.loggerFor(ctx).Error().Err(err).
				Str("event", "openwebif.decode").
				Str("operation", ep.operation).
				Str("bouquet_ref", maskedRef).
				Msg("failed to decode services response shape")
			return nil, fmt.Errorf("%w: %v", errServicesSchemaMismatch, err)
		}
		trimmedServices := strings.TrimSpace(string(shape.Services))
		if trimmedServices == "" || trimmedServices == "null" {
			// Legacy receivers may respond to getallservices with only a bouquets payload.
			// When this happens during fallback, treat it as an empty service list.
			if ep.preferFlat {
				var legacyShape struct {
					Bouquets json.RawMessage `json:"bouquets"`
				}
				if err := json.Unmarshal(body, &legacyShape); err == nil {
					trimmedBouquets := strings.TrimSpace(string(legacyShape.Bouquets))
					if trimmedBouquets != "" && trimmedBouquets != "null" {
						return [][2]string{}, nil
					}
				}
			}
			return nil, fmt.Errorf("%w: missing services field", errServicesSchemaMismatch)
		}

		// Flat endpoint: decode preserving subservices.
		if ep.preferFlat {
			var flat svcPayloadFlat
			if err := json.Unmarshal(body, &flat); err != nil {
				c.loggerFor(ctx).Error().Err(err).
					Str("event", "openwebif.decode").
					Str("operation", ep.operation).
					Str("bouquet_ref", maskedRef).
					Msg("failed to decode services response (flat)")
				return nil, fmt.Errorf("%w: %v", errServicesSchemaMismatch, err)
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
				Str("operation", ep.operation).
				Str("bouquet_ref", maskedRef).
				Msg("failed to decode services response")
			return nil, fmt.Errorf("%w: %v", errServicesSchemaMismatch, err)
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

	endpoints := []endpointSpec{
		{
			path:       "/api/getservices?sRef=" + url.QueryEscape(bouquetRef),
			operation:  "services.nested",
			preferFlat: false,
		},
		{
			path:       "/api/getallservices?sRef=" + url.QueryEscape(bouquetRef),
			operation:  "services.flat",
			preferFlat: true,
		},
	}
	if preferFlat, ok := c.servicesCapabilityGet(); ok && preferFlat {
		endpoints[0], endpoints[1] = endpoints[1], endpoints[0]
	}

	var empty [][2]string
	var successfulEndpoint string

	for i, ep := range endpoints {
		out, err := try(ep)
		if err != nil {
			if i == 0 && shouldTryServicesFallback(err) {
				c.loggerFor(ctx).Warn().
					Err(err).
					Str("event", "openwebif.services").
					Str("bouquet_ref", maskedRef).
					Str("from", ep.operation).
					Str("to", endpoints[1].operation).
					Msg("services fetch incompatible, trying fallback endpoint")
				continue
			}
			// A primary endpoint already returned a valid (legitimately empty) payload;
			// only the fallback failed. Do not discard the empty-but-successful primary
			// result and abort the whole refresh — fall through to the empty-result path.
			if successfulEndpoint != "" {
				c.loggerFor(ctx).Warn().
					Err(err).
					Str("event", "openwebif.services").
					Str("bouquet_ref", maskedRef).
					Str("operation", ep.operation).
					Msg("fallback endpoint failed after primary returned empty; using primary result")
				break
			}
			return nil, err
		}

		successfulEndpoint = ep.operation
		c.servicesCapabilitySet(ep.preferFlat)
		if len(out) > 0 {
			if c.cache != nil {
				c.cache.Set(cacheKey, out, c.cacheTTL)
				c.loggerFor(ctx).Debug().Str("event", "cache.set").Str("key", cacheKey).Dur("ttl", c.cacheTTL).Msg("cached result")
			}
			c.loggerFor(ctx).Info().
				Str("event", "openwebif.services").
				Str("bouquet_ref", maskedRef).
				Str("operation", ep.operation).
				Int("count", len(out)).
				Msg("fetched services")
			return out, nil
		}

		empty = out
		if i == 0 {
			continue
		}
	}

	// No services found but at least one endpoint returned a compatible payload.
	if successfulEndpoint != "" {
		if empty == nil {
			empty = [][2]string{}
		}
		if c.cache != nil {
			c.cache.Set(cacheKey, empty, c.cacheTTL)
		}
		c.loggerFor(ctx).Warn().
			Str("event", "openwebif.services").
			Str("bouquet_ref", maskedRef).
			Str("operation", successfulEndpoint).
			Msg("no services found for bouquet")
		return empty, nil
	}

	return nil, fmt.Errorf("%w: no compatible services endpoint", errServicesSchemaMismatch)
}

func shouldTryServicesFallback(err error) bool {
	return errors.Is(err, errServicesSchemaMismatch) || errors.Is(err, ErrNotFound)
}

func (c *Client) servicesCapabilityKey() string {
	return c.host
}

func (c *Client) servicesCapabilityGet() (bool, bool) {
	key := c.servicesCapabilityKey()
	now := time.Now()

	c.servicesCapMu.RLock()
	cap, ok := c.servicesCaps[key]
	c.servicesCapMu.RUnlock()
	if ok && now.Before(cap.ExpiresAt) {
		return cap.PreferFlat, true
	}

	// Entry is missing or expired. Re-check under write lock to avoid deleting
	// a freshly refreshed capability.
	c.servicesCapMu.Lock()
	defer c.servicesCapMu.Unlock()
	cap, ok = c.servicesCaps[key]
	if ok && time.Now().Before(cap.ExpiresAt) {
		return cap.PreferFlat, true
	}
	if ok {
		delete(c.servicesCaps, key)
	}
	return false, false
}

func (c *Client) servicesCapabilitySet(preferFlat bool) {
	if c.servicesCapTTL <= 0 {
		return
	}
	key := c.servicesCapabilityKey()
	exp := time.Now().Add(c.servicesCapTTL)

	c.servicesCapMu.Lock()
	c.servicesCaps[key] = servicesCapability{
		PreferFlat: preferFlat,
		ExpiresAt:  exp,
	}
	c.servicesCapMu.Unlock()
}
