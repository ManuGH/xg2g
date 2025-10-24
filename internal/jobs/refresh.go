// SPDX-License-Identifier: MIT

// Package jobs provides background job execution functionality.
package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/ManuGH/xg2g/internal/validate"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ErrInvalidStreamPort marks an invalid stream port configuration.
var ErrInvalidStreamPort = errors.New("invalid stream port")

// Status represents the current state of the refresh job
type Status struct {
	Version       string    `json:"version"`
	LastRun       time.Time `json:"lastRun"`
	Channels      int       `json:"channels"`
	Bouquets      int       `json:"bouquets,omitempty"`      // Number of bouquets processed
	EPGProgrammes int       `json:"epgProgrammes,omitempty"` // Number of EPG programmes collected
	DurationMS    int64     `json:"durationMs,omitempty"`    // Duration of last refresh in milliseconds
	Error         string    `json:"error,omitempty"`
}

// Config holds configuration for refresh operations
type Config struct {
	Version       string
	DataDir       string
	OWIBase       string
	OWIUsername   string // Optional: HTTP Basic Auth username
	OWIPassword   string // Optional: HTTP Basic Auth password
	Bouquet       string // Comma-separated list of bouquets (e.g., "Premium,Favourites")
	XMLTVPath     string
	PiconBase     string
	FuzzyMax      int
	StreamPort    int
	APIToken      string // Optional: for securing the /api/refresh endpoint
	OWITimeout    time.Duration
	OWIRetries    int
	OWIBackoff    time.Duration
	OWIMaxBackoff time.Duration

	// EPG Configuration
	EPGEnabled        bool
	EPGDays           int // Number of days to fetch EPG data (1-14)
	EPGMaxConcurrency int // Max parallel EPG requests (1-10)
	EPGTimeoutMS      int // Timeout per EPG request in milliseconds
	EPGRetries        int // Retry attempts for EPG requests
}

// Refresh performs the complete refresh cycle: fetch bouquets → services → write M3U + XMLTV
//
//nolint:gocyclo // Complex orchestration function with validation, requires sequential operations
func Refresh(ctx context.Context, cfg Config) (*Status, error) {
	// Start tracing span for the entire refresh job
	tracer := telemetry.Tracer("xg2g.jobs")
	ctx, span := tracer.Start(ctx, "job.refresh",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	startTime := time.Now()

	logger := xglog.WithComponentFromContext(ctx, "jobs")
	logger.Info().Str("event", "refresh.start").Msg("starting refresh")

	if err := validateConfig(cfg); err != nil {
		metrics.IncConfigValidationError()
		metrics.IncRefreshFailure("config")
		span.RecordError(err)
		span.SetStatus(codes.Error, "config validation failed")
		return nil, err
	}

	opts := openwebif.Options{
		Timeout:    cfg.OWITimeout,
		MaxRetries: cfg.OWIRetries,
		Backoff:    cfg.OWIBackoff,
		MaxBackoff: cfg.OWIMaxBackoff,
		Username:   cfg.OWIUsername,
		Password:   cfg.OWIPassword,
	}
	client := openwebif.NewWithPort(cfg.OWIBase, cfg.StreamPort, opts)

	// Fetch bouquets with tracing
	span.AddEvent("fetching bouquets")
	bouquets, err := client.Bouquets(ctx)
	if err != nil {
		metrics.IncRefreshFailure("bouquets")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch bouquets")
		return nil, fmt.Errorf("failed to fetch bouquets: %w", err)
	}
	metrics.RecordBouquetsCount(len(bouquets))
	span.SetAttributes(attribute.Int("bouquets.count", len(bouquets)))

	// Support comma-separated bouquet list (e.g., "Premium,Favourites,Sports")
	requestedBouquets := strings.Split(cfg.Bouquet, ",")
	for i := range requestedBouquets {
		requestedBouquets[i] = strings.TrimSpace(requestedBouquets[i])
	}

	// Pre-flight validation: check ALL requested bouquets exist before processing
	var missingBouquets []string
	validBouquets := make([]string, 0, len(requestedBouquets))
	for _, bouquetName := range requestedBouquets {
		if bouquetName == "" {
			continue
		}
		if _, ok := bouquets[bouquetName]; !ok {
			missingBouquets = append(missingBouquets, bouquetName)
		} else {
			validBouquets = append(validBouquets, bouquetName)
		}
	}

	// If ANY requested bouquet is missing, fail early with comprehensive error
	if len(missingBouquets) > 0 {
		metrics.IncRefreshFailure("bouquets")
		availableNames := make([]string, 0, len(bouquets))
		for name := range bouquets {
			availableNames = append(availableNames, name)
		}
		return nil, fmt.Errorf("bouquets not found: %v; available bouquets: %v", missingBouquets, availableNames)
	}

	var items []playlist.Item
	// Channel type counters for the last refresh
	hd, sd, radio, unknown := 0, 0, 0, 0
	// Channel counter for tvg-chno (position across all bouquets)
	// This ensures Threadfin/Plex display channels in bouquet order
	channelNumber := 1

	for _, bouquetName := range validBouquets {
		bouquetRef := bouquets[bouquetName] // Safe: already validated above

		services, err := client.Services(ctx, bouquetRef)
		if err != nil {
			metrics.IncRefreshFailure("services")
			return nil, fmt.Errorf("failed to fetch services for bouquet %q: %w", bouquetName, err)
		}
		metrics.RecordServicesCount(bouquetName, len(services))

		for _, s := range services {
			name, ref := s[0], s[1]
			streamURL, err := client.StreamURL(ref, name)
			if err != nil {
				logger.Warn().Err(err).Str("service", name).Msg("failed to build stream URL")
				metrics.IncStreamURLBuild("failure")
				metrics.IncRefreshFailure("streamurl")
				continue
			}
			metrics.IncStreamURLBuild("success")

			// Validate stream URL structure
			validator := validate.New()
			validator.StreamURL("streamURL", streamURL)
			if !validator.IsValid() {
				logger.Warn().
					Str("service", name).
					Str("url", streamURL).
					Str("validation_error", validator.Err().Error()).
					Msg("stream URL validation failed")
				metrics.IncStreamURLBuild("validation_failure")
				continue
			}

			piconURL := ""
			if cfg.PiconBase != "" {
				piconURL = openwebif.PiconURL(cfg.PiconBase, ref)
			}

			// Naive channel type classification based on name/ref hints.
			lr := strings.ToLower(name + " " + ref)
			switch {
			case strings.Contains(lr, "radio") || strings.HasPrefix(ref, "1:0:2:"):
				radio++
			case strings.Contains(lr, "hd"):
				hd++
			case strings.Contains(lr, "sd"):
				sd++
			default:
				unknown++
			}

			items = append(items, playlist.Item{
				Name:    name,
				TvgID:   makeStableIDFromSRef(ref),
				TvgChNo: channelNumber, // Sequential numbering based on bouquet position
				TvgLogo: piconURL,
				Group:   bouquetName, // Use actual bouquet name as group
				URL:     streamURL,
			})
			channelNumber++
		}
	}
	metrics.RecordChannelTypeCounts(hd, sd, radio, unknown)

	// Write M3U playlist (filename configurable via ENV)
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	// Sanitize filename for security
	safeName, err := sanitizeFilename(playlistName)
	if err != nil {
		metrics.IncRefreshFailure("sanitize_filename")
		return nil, fmt.Errorf("invalid playlist filename: %w", err)
	}
	playlistPath := filepath.Join(cfg.DataDir, safeName)
	if err := writeM3U(ctx, playlistPath, items); err != nil {
		metrics.IncRefreshFailure("write_m3u")
		metrics.RecordPlaylistFileValidity("m3u", false)
		return nil, fmt.Errorf("failed to write M3U playlist: %w", err)
	}
	// Verify M3U file exists and is readable
	if _, err := os.Stat(playlistPath); err == nil {
		metrics.RecordPlaylistFileValidity("m3u", true)
	} else {
		metrics.RecordPlaylistFileValidity("m3u", false)
	}
	logger.Info().
		Str("event", "playlist.write").
		Str("path", playlistPath).
		Int("channels", len(items)).
		Msg("playlist written")

	// Track EPG programmes count for status reporting
	var epgProgrammesCount int

	// Optional XMLTV generation
	if cfg.XMLTVPath != "" {
		xmlCh := make([]epg.Channel, 0, len(items))
		for _, it := range items {
			ch := epg.Channel{ID: it.TvgID, DisplayName: []string{it.Name}}
			if it.TvgLogo != "" {
				ch.Icon = &epg.Icon{Src: it.TvgLogo}
			}
			xmlCh = append(xmlCh, ch)
		}

		xmltvFullPath := filepath.Join(cfg.DataDir, cfg.XMLTVPath)
		var xmlErr error
		var allProgrammes []epg.Programme

		// EPG Programme collection (if enabled)
		if cfg.EPGEnabled {
			logger.Info().
				Str("event", "epg.start").
				Int("channels", len(items)).
				Int("days", cfg.EPGDays).
				Msg("starting EPG collection")

			epgStartTime := time.Now()
			programmes := collectEPGProgrammes(ctx, client, items, cfg)
			epgDuration := time.Since(epgStartTime).Seconds()

			if len(programmes) == 0 {
				logger.Warn().
					Str("event", "epg.no_data").
					Msg("EPG collection returned no data")
			}
			allProgrammes = programmes
			epgProgrammesCount = len(allProgrammes)

			// Count channels with EPG data
			channelsWithData := 0
			if len(allProgrammes) > 0 {
				channelMap := make(map[string]bool)
				for _, prog := range allProgrammes {
					channelMap[prog.Channel] = true
				}
				channelsWithData = len(channelMap)
			}

			metrics.RecordEPGCollection(len(allProgrammes), channelsWithData, epgDuration)

			logger.Info().
				Str("event", "epg.collected").
				Int("programmes", len(allProgrammes)).
				Int("channels_with_data", channelsWithData).
				Float64("duration_seconds", epgDuration).
				Msg("EPG collection completed")
		}

		// Write XMLTV with or without programmes
		var tv epg.TV
		if cfg.EPGEnabled && len(allProgrammes) > 0 {
			tv = epg.GenerateXMLTV(xmlCh, allProgrammes)
		} else {
			tv = epg.GenerateXMLTV(xmlCh, nil)
		}
		xmlErr = epg.WriteXMLTV(tv, xmltvFullPath)

		metrics.RecordXMLTV(true, len(xmlCh), xmlErr)
		if xmlErr != nil {
			metrics.IncRefreshFailure("xmltv")
			metrics.RecordPlaylistFileValidity("xmltv", false)
			return nil, fmt.Errorf("failed to write XMLTV file to %q: %w", xmltvFullPath, xmlErr)
		}
		// Verify XMLTV file exists and is readable
		if _, err := os.Stat(xmltvFullPath); err == nil {
			metrics.RecordPlaylistFileValidity("xmltv", true)
		} else {
			metrics.RecordPlaylistFileValidity("xmltv", false)
		}

		logger.Info().
			Str("event", "xmltv.success").
			Str("path", xmltvFullPath).
			Int("channels", len(xmlCh)).
			Int("programmes", len(allProgrammes)).
			Msg("XMLTV generated")
	} else {
		metrics.RecordXMLTV(false, 0, nil)
		metrics.RecordPlaylistFileValidity("xmltv", false) // XMLTV disabled
	}

	// Calculate job duration
	duration := time.Since(startTime)

	// Create detailed status response
	status := &Status{
		LastRun:       time.Now(),
		Channels:      len(items),
		Bouquets:      len(validBouquets),
		EPGProgrammes: epgProgrammesCount,
		DurationMS:    duration.Milliseconds(),
	}
	// Add attributes for tracing
	span.SetAttributes(
		attribute.Int("channels.total", status.Channels),
		attribute.Int64("duration_ms", duration.Milliseconds()),
	)
	span.SetStatus(codes.Ok, "refresh completed successfully")

	logger.Info().
		Str("event", "refresh.success").
		Int("channels", status.Channels).
		Msg("refresh completed")
	return status, nil
}

// writeM3U safely writes the playlist to a temporary file and renames it on success.
func writeM3U(ctx context.Context, path string, items []playlist.Item) error {
	logger := xglog.FromContext(ctx)
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "playlist-*.m3u.tmp")
	if err != nil {
		return fmt.Errorf("create temporary M3U file: %w", err)
	}
	// Defer a function to handle cleanup, logging any errors.
	closed := false
	defer func() {
		if !closed {
			if err := tmpFile.Close(); err != nil {
				logger.Warn().Err(err).Str("path", tmpFile.Name()).Msg("failed to close temporary file on error path")
			}
		}
		// Only remove the temp file if it still exists (i.e., rename failed).
		if _, statErr := os.Stat(tmpFile.Name()); !os.IsNotExist(statErr) {
			if err := os.Remove(tmpFile.Name()); err != nil {
				logger.Warn().Err(err).Str("path", tmpFile.Name()).Msg("failed to remove temporary file")
			}
		}
	}()

	if err := playlist.WriteM3U(tmpFile, items); err != nil {
		return fmt.Errorf("write to temporary M3U file: %w", err)
	}

	// Explicitly close before rename.
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary M3U file before rename: %w", err)
	}
	closed = true

	// Atomically rename the temporary file to the final destination
	if err := os.Rename(tmpFile.Name(), path); err != nil {
		return fmt.Errorf("rename temporary M3U file: %w", err)
	}

	return nil
}

// makeStableIDFromSRef creates a deterministic, collision-resistant tvg-id from a service reference.
// Using a hash ensures the ID is stable even if the channel name changes and avoids issues
// with special characters in the sRef.
func makeStableIDFromSRef(sref string) string {
	sum := sha256.Sum256([]byte(sref))
	return "sref-" + hex.EncodeToString(sum[:])
}

func validateConfig(cfg Config) error {
	// Use centralized validation package
	v := validate.New()

	v.URL("OWIBase", cfg.OWIBase, []string{"http", "https"})
	v.Port("StreamPort", cfg.StreamPort)
	v.Directory("DataDir", cfg.DataDir, false)

	if !v.IsValid() {
		return v.Err()
	}

	return nil
}

// collectEPGProgrammes fetches EPG data using per-service requests with bounded concurrency
func collectEPGProgrammes(ctx context.Context, client *openwebif.Client, items []playlist.Item, cfg Config) []epg.Programme {
	logger := xglog.FromContext(ctx)

	// Clamp concurrency to sane bounds [1,10]
	maxPar := cfg.EPGMaxConcurrency
	if maxPar < 1 {
		maxPar = 1
	}
	if maxPar > 10 {
		maxPar = 10
	}

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

		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
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

type epgResult struct {
	channelID string
	events    []openwebif.EPGEvent
	err       error
}

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

// sanitizeFilename sanitizes a playlist filename to prevent path traversal attacks
func sanitizeFilename(name string) (string, error) {
	if name == "" {
		return "playlist.m3u", nil
	}

	// Strip any directory components
	base := filepath.Base(name)

	// Reject if still contains traversal
	if strings.Contains(base, "..") {
		return "", fmt.Errorf("invalid filename: contains traversal")
	}

	// Clean the filename
	cleaned := filepath.Clean(base)

	// Ensure it's local
	if !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("invalid filename: not local")
	}

	// Validate extension
	ext := filepath.Ext(cleaned)
	if ext != ".m3u" && ext != ".m3u8" {
		cleaned += ".m3u"
	}

	return cleaned, nil
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
