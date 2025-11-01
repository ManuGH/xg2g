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

// collectEPGProgrammes fetches EPG data using per-service requests with bounded concurrency
func collectEPGProgrammes(ctx context.Context, client *openwebif.Client, items []playlist.Item, cfg Config) []epg.Programme {
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
