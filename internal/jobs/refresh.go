// SPDX-License-Identifier: MIT

// Package jobs provides background job execution functionality.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
