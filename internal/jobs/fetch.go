// SPDX-License-Identifier: MIT

package jobs

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
)

// epgResult holds the result of a single EPG fetch operation
type epgResult struct {
	channelID string
	events    []openwebif.EPGEvent
	err       error
}

// collectEPGProgrammes is the main entry point for EPG collection.
// It routes to either bouquet-based or per-service fetching based on cfg.EPGSource
func collectEPGProgrammes(ctx context.Context, client *openwebif.Client, items []playlist.Item, cfg Config) []epg.Programme {
	logger := xglog.FromContext(ctx)

	// Route to appropriate EPG collection strategy
	if cfg.EPGSource == "bouquet" {
		logger.Info().Msg("Using bouquet-based EPG fetch strategy")
		return collectEPGFromBouquet(ctx, client, items, cfg)
	}

	// Default: per-service strategy
	logger.Info().Msg("Using per-service EPG fetch strategy")
	return collectEPGPerService(ctx, client, items, cfg)
}

// collectEPGFromBouquet fetches EPG for all channels in one request (faster, single API call)
func collectEPGFromBouquet(ctx context.Context, client *openwebif.Client, items []playlist.Item, cfg Config) []epg.Programme {
	logger := xglog.FromContext(ctx)

	// Extract bouquet reference from first channel's stream URL
	// All channels in the same bouquet share the bouquet reference
	var bouquetRef string
	for _, item := range items {
		sRef := extractSRefFromStreamURL(item.URL)
		if sRef != "" {
			// Extract bouquet ref from service ref (format: 1:7:1:0:0:0:0:0:0:0:FROM BOUQUET "userbouquet.xxx.tv" ORDER BY bouquet)
			// For now, we'll use cfg.Bouquet to look up the bouquet ref
			break
		}
	}

	// Fetch EPG for entire bouquet in one request
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.EPGTimeoutMS*len(items))*time.Millisecond)
	defer cancel()

	// Get bouquets to find the reference
	bouquets, err := client.Bouquets(reqCtx)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to fetch bouquets for EPG")
		return nil
	}

	// Find the bouquet reference for the configured bouquet name
	for name, ref := range bouquets {
		if name == cfg.Bouquet {
			bouquetRef = ref
			break
		}
	}

	if bouquetRef == "" {
		logger.Warn().Str("bouquet", cfg.Bouquet).Msg("Bouquet not found, falling back to per-service EPG")
		return collectEPGPerService(ctx, client, items, cfg)
	}

	logger.Debug().Str("bouquet_ref", bouquetRef).Msg("Fetching EPG for bouquet")

	// Fetch all EPG events for the bouquet
	events, err := client.GetBouquetEPG(reqCtx, bouquetRef, cfg.EPGDays)
	if err != nil {
		logger.Error().Err(err).Str("bouquet", cfg.Bouquet).Msg("Failed to fetch bouquet EPG")
		return nil
	}

	logger.Info().Int("raw_events", len(events)).Msg("Received EPG events from bouquet")

	// Now we need to match EPG events to playlist items using SRef
	// Build a map of service references for exact matching
	srefMap := make(map[string]string) // sref -> tvg-id
	for _, item := range items {
		sref := extractSRefFromStreamURL(item.URL)
		if sref != "" {
			srefMap[sref] = item.TvgID
		}
	}

	// Match events to channels and convert to XMLTV programmes
	var allProgrammes []epg.Programme
	eventsByChannel := make(map[string][]openwebif.EPGEvent)

	for _, event := range events {
		// Match by service reference (exact match)
		tvgID, found := srefMap[event.SRef]

		if !found {
			logger.Debug().Str("sref", event.SRef).Msg("No channel match for EPG event")
			continue
		}

		eventsByChannel[tvgID] = append(eventsByChannel[tvgID], event)
	}

	// Convert grouped events to programmes
	channelsWithData := 0
	for channelID, channelEvents := range eventsByChannel {
		if len(channelEvents) > 0 {
			channelsWithData++
		}
		progs := epg.ProgrammesFromEPG(channelEvents, channelID)
		allProgrammes = append(allProgrammes, progs...)
	}

	logger.Info().
		Int("total_programmes", len(allProgrammes)).
		Int("channels_with_data", channelsWithData).
		Int("total_channels", len(items)).
		Msg("EPG collected via bouquet endpoint")

	metrics.RecordEPGChannelSuccess(channelsWithData)

	return allProgrammes
}

// collectEPGPerService fetches EPG data using per-service requests with bounded concurrency
func collectEPGPerService(ctx context.Context, client *openwebif.Client, items []playlist.Item, cfg Config) []epg.Programme {
	logger := xglog.FromContext(ctx)

	// Clamp concurrency to sane bounds [1,10]
	maxPar := clampConcurrency(cfg.EPGMaxConcurrency, 5, 10)

	// Worker pool semaphore
	sem := make(chan struct{}, maxPar)
	results := make(chan epgResult, len(items))
	var wg sync.WaitGroup

	// Schedule per-channel EPG fetches
	for _, item := range items {
		it := item // capture
		// Validate sRef presence early to avoid spinning up a goroutine needlessly
		if sref := extractSRefFromStreamURL(it.URL); sref == "" {
			logger.Debug().Str("channel", it.Name).Msg("skipping EPG: could not extract sRef from stream URL")
			continue
		}

		// Use Go 1.25 WaitGroup.Go() for safer goroutine management
		wg.Go(func() {
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Deadline per request
			reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.EPGTimeoutMS)*time.Millisecond)
			defer cancel()

			events, err := fetchEPGWithRetry(reqCtx, client, it.URL, cfg)
			if err != nil {
				logger.Debug().Err(err).
					Str("channel", it.Name).
					Str("tvg_id", it.TvgID).
					Msg("EPG fetch failed for channel")
				results <- epgResult{channelID: it.TvgID, events: nil, err: err}
				return
			}
			results <- epgResult{channelID: it.TvgID, events: events, err: nil}
		})
	}

	// Close results when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Aggregate results
	var allProgrammes []epg.Programme
	channelsWithData := 0

	for res := range results {
		if res.err != nil {
			// already logged
			continue
		}
		if len(res.events) > 0 {
			channelsWithData++
		}
		progs := epg.ProgrammesFromEPG(res.events, res.channelID)
		if len(progs) > 0 {
			allProgrammes = append(allProgrammes, progs...)
		}
	}

	logger.Info().
		Int("total_programmes", len(allProgrammes)).
		Int("channels_with_data", channelsWithData).
		Int("concurrency", maxPar).
		Msg("EPG collected via service endpoints")

	metrics.RecordEPGChannelSuccess(channelsWithData)

	return allProgrammes
}

// fetchEPGWithRetry attempts to fetch EPG data with exponential backoff retry
func fetchEPGWithRetry(ctx context.Context, client *openwebif.Client, streamURL string, cfg Config) ([]openwebif.EPGEvent, error) {
	// Extract sRef from streamURL - adjust based on your stream URL format
	sRef := extractSRefFromStreamURL(streamURL)
	if sRef == "" {
		return nil, fmt.Errorf("cannot extract sRef from stream URL: %s", streamURL)
	}

	var lastErr error
	for attempt := 0; attempt <= cfg.EPGRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(attempt*attempt*500) * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		events, err := client.GetEPG(ctx, sRef, cfg.EPGDays)
		if err == nil {
			return events, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("EPG request failed after %d retries: %w", cfg.EPGRetries, lastErr)
}

// extractSRefFromStreamURL extracts service reference from stream URL
func extractSRefFromStreamURL(streamURL string) string {
	u, err := url.Parse(streamURL)
	if err != nil {
		return ""
	}

	// New format (direct service reference): http://host:port/1:0:19:132F:3EF:1:C00000:0:0:0:
	// Service reference is in the path
	path := strings.TrimPrefix(u.Path, "/")
	if path != "" && strings.Contains(path, ":") {
		return path
	}

	// Old format (fallback): http://host:port/web/stream.m3u?ref=ENCODED_SREF
	encodedRef := u.Query().Get("ref")
	if encodedRef == "" {
		return ""
	}

	decodedRef, err := url.QueryUnescape(encodedRef)
	if err != nil {
		return ""
	}

	return decodedRef
}
