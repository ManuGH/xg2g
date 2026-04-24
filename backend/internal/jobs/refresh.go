// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

// Package jobs provides background job execution functionality.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/epg"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/platform/paths"
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
	Version string `json:"version"`

	LastRun time.Time `json:"lastRun"`

	Channels int `json:"channels"`

	Bouquets int `json:"bouquets,omitempty"` // Number of bouquets processed

	EPGProgrammes int `json:"epgProgrammes,omitempty"` // Number of EPG programmes collected

	DurationMS int64 `json:"durationMs,omitempty"` // Duration of last refresh in milliseconds

	Error string `json:"error,omitempty"`
}

type RefreshOption func(*refreshOptions)

type refreshOptions struct {
	piconPool *PiconPool
}

type resolvedBouquet struct {
	Name string
	Ref  string
}

func WithPiconPool(pool *PiconPool) RefreshOption {
	return func(opts *refreshOptions) {
		opts.piconPool = pool
	}
}

func buildRefreshOptions(opts []RefreshOption) refreshOptions {
	var cfg refreshOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func Refresh(ctx context.Context, snap config.Snapshot) (*Status, error) {
	return RefreshWithOptions(ctx, snap)
}

// RefreshWithOptions performs the complete refresh cycle: fetch bouquets → services → write M3U + XMLTV.
//
//nolint:gocyclo // Complex orchestration function with validation, requires sequential operations
func RefreshWithOptions(ctx context.Context, snap config.Snapshot, opts ...RefreshOption) (*Status, error) {
	// Start tracing span for the entire refresh job
	tracer := telemetry.Tracer("xg2g.jobs")
	ctx, span := tracer.Start(ctx, "job.refresh",
		trace.WithSpanKind(trace.SpanKindInternal),
	)
	defer span.End()

	cfg := snap.App
	rt := snap.Runtime
	refreshOpts := buildRefreshOptions(opts)

	startTime := time.Now()

	logger := xglog.WithComponentFromContext(ctx, "jobs")
	logger.Info().Str("event", "refresh.start").Msg("starting refresh")

	if err := validateConfig(cfg); err != nil {
		err = WrapRefreshConfigError(err)
		metrics.IncConfigValidationError()
		metrics.IncRefreshFailure("config")
		logJobError("refresh", logger.Error().Err(err).Str("event", "refresh.failed").Str("stage", "config"), err).Msg("refresh failed")
		span.RecordError(err)
		span.SetStatus(codes.Error, "config validation failed")
		return nil, err
	}

	owiOpts := openwebif.Options{
		Timeout:         cfg.Enigma2.Timeout,
		MaxRetries:      cfg.Enigma2.Retries,
		Backoff:         cfg.Enigma2.Backoff,
		MaxBackoff:      cfg.Enigma2.MaxBackoff,
		Username:        cfg.Enigma2.Username,
		Password:        cfg.Enigma2.Password,
		UseWebIFStreams: cfg.Enigma2.UseWebIFStreams,
		StreamBaseURL:   rt.OpenWebIF.StreamBaseURL,

		HTTPMaxConnsPerHost: rt.OpenWebIF.HTTPMaxConnsPerHost,
	}
	client := openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, owiOpts)

	// Fetch bouquets with tracing
	span.AddEvent("fetching bouquets")
	bouquets, err := client.Bouquets(ctx)
	if err != nil {
		err = WrapBouquetsFetchError(fmt.Errorf("failed to fetch bouquets: %w", err))
		metrics.IncRefreshFailure("bouquets")
		logJobError("refresh", logger.Error().Err(err).Str("event", "refresh.failed").Str("stage", "bouquets"), err).Msg("failed to fetch bouquets")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to fetch bouquets")
		return nil, err
	}
	metrics.RecordBouquetsCount(len(bouquets))
	span.SetAttributes(attribute.Int("bouquets.count", len(bouquets)))

	validBouquets, usingAllBouquets, err := resolveRefreshBouquets(cfg.Bouquet, bouquets)
	if usingAllBouquets {
		logger.Info().
			Int("bouquets", len(validBouquets)).
			Msg("no bouquets configured; using all available bouquets")
	}
	if err != nil {
		metrics.IncRefreshFailure("bouquets")
		logJobError("refresh", logger.Error().Err(err).Str("event", "refresh.failed").Str("stage", "bouquets"), err).Msg("configured bouquets not found")
		return nil, err
	}

	var items []playlist.Item
	// Channel type counters for the last refresh
	hd, sd, radio, unknown := 0, 0, 0, 0
	// Channel counter for tvg-chno (position across all bouquets)
	// This ensures Threadfin/Plex display channels in bouquet order
	channelNumber := 1

	for _, b := range validBouquets {
		bouquetName := b.Name
		bouquetRef := b.Ref

		services, err := client.Services(ctx, bouquetRef)
		if err != nil {
			err = WrapServicesFetchError(fmt.Errorf("failed to fetch services for bouquet %q: %w", bouquetName, err))
			metrics.IncRefreshFailure("services")
			logJobError("refresh", logger.Error().Err(err).Str("event", "refresh.failed").Str("stage", "services").Str("bouquet", bouquetName), err).Msg("failed to fetch services")
			return nil, err
		}
		metrics.RecordServicesCount(bouquetName, len(services))

		for _, s := range services {
			name, ref := s[0], s[1]
			streamURL, err := client.StreamURL(ctx, ref, name)
			if err != nil {
				err = WrapStreamURLBuildError(fmt.Errorf("failed to build stream URL for %q: %w", name, err))
				logJobError("refresh", logger.Warn().Err(err).Str("service", name), err).Msg("failed to build stream URL")
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
					Str("url_host", safeURLHost(streamURL)).
					Str("validation_error", validator.Err().Error()).
					Msg("stream URL validation failed")
				metrics.IncStreamURLBuild("validation_failure")
				continue
			}

			// If XG2G_USE_PROXY_URLS=true, rewrite URLs to point to xg2g proxy
			// This enables audio transcoding and smart stream detection for Plex/Jellyfin
			if rt.UseProxyURLs {
				// Always rewrite using the known service reference (avoids dropping query params for WebIF URLs).
				proxyBase := strings.TrimRight(rt.ProxyBaseURL, "/")
				streamURL = proxyBase + "/" + ref
			}

			// Use local proxy for picons to avoid Mixed Content / CORS issues
			// Use underscore-based naming for browser compatibility
			piconRef := strings.ReplaceAll(ref, ":", "_")
			piconRef = strings.TrimRight(piconRef, "_")

			// Use relative URL for internal components (WebUI, API)
			// The M3U writer will prepend the public URL if configured
			logoURL := fmt.Sprintf("/logos/%s.png?v=%d", piconRef, time.Now().Unix())

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
				Name:       name,
				TvgID:      ref,           // Use ServiceRef as TvgID (raw numbers preferred by user)
				TvgChNo:    channelNumber, // Sequential numbering based on bouquet position
				TvgLogo:    logoURL,
				Group:      bouquetName, // Use actual bouquet name as group
				URL:        streamURL,
				ServiceRef: ref, // Explicitly store ref for EPG fetching
			})
			channelNumber++
		}
	}
	metrics.RecordChannelTypeCounts(hd, sd, radio, unknown)

	if err := writeRefreshPlaylist(ctx, cfg, rt, items, refreshOpts); err != nil {
		return nil, err
	}

	epgProgrammesCount, err := writeRefreshXMLTV(ctx, cfg, rt, client, items)
	if err != nil {
		return nil, err
	}

	// Calculate job duration
	duration := time.Since(startTime)

	// Create detailed status response
	status := &Status{
		Version:       cfg.Version,
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

func resolveRefreshBouquets(configured string, bouquets map[string]string) ([]resolvedBouquet, bool, error) {
	bouquetRefs := make(map[string]string, len(bouquets))
	for name, ref := range bouquets {
		bouquetRefs[ref] = name
	}

	requestedBouquets := requestedRefreshBouquets(configured)
	usingAllBouquets := len(requestedBouquets) == 0
	if usingAllBouquets {
		for name := range bouquets {
			requestedBouquets = append(requestedBouquets, name)
		}
		sort.Strings(requestedBouquets)
	}

	validBouquets := make([]resolvedBouquet, 0, len(requestedBouquets))
	missingBouquets := make([]string, 0)
	for _, bouquetName := range requestedBouquets {
		if ref, ok := bouquets[bouquetName]; ok {
			validBouquets = append(validBouquets, resolvedBouquet{Name: bouquetName, Ref: ref})
			continue
		}

		if name, ok := bouquetRefs[bouquetName]; ok {
			validBouquets = append(validBouquets, resolvedBouquet{Name: name, Ref: bouquetName})
			continue
		}

		missingBouquets = append(missingBouquets, bouquetName)
	}

	if len(missingBouquets) > 0 {
		availableNames := make([]string, 0, len(bouquets))
		for name := range bouquets {
			availableNames = append(availableNames, name)
		}
		return nil, usingAllBouquets, WrapBouquetNotFoundError(fmt.Errorf("bouquets not found: %v; available bouquets: %v", missingBouquets, availableNames))
	}

	return validBouquets, usingAllBouquets, nil
}

func requestedRefreshBouquets(configured string) []string {
	requested := make([]string, 0)
	for _, name := range strings.Split(configured, ",") {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			requested = append(requested, trimmed)
		}
	}
	return requested
}

func writeRefreshPlaylist(ctx context.Context, cfg config.AppConfig, rt config.RuntimeSnapshot, items []playlist.Item, opts refreshOptions) error {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

	playlistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, rt.PlaylistFilename)
	if err != nil {
		err = WrapPlaylistPathError(fmt.Errorf("invalid playlist path: %w", err))
		logJobError("refresh", logger.Error().Err(err).Str("playlist", rt.PlaylistFilename), err).Msg("invalid playlist path")
		metrics.IncRefreshFailure("playlist_path_invalid")
		return err
	}

	// The runtime pool can fall back to the Enigma2 base URL even when no explicit
	// PiconBase is configured, so gate this on the runtime pool, not on cfg.PiconBase.
	if opts.piconPool != nil {
		go PrewarmPicons(ctx, opts.piconPool, items)
	} else if cfg.PiconBase != "" {
		logger.Debug().Msg("skipping picon pre-warm because no runtime picon pool is configured")
	}

	// Pass Public URL to M3U writer for absolute paths in M3U (Plex compatibility).
	// WebUI uses relative paths internally.
	if err := writeM3U(ctx, playlistPath, items, rt.PublicURL, rt.XTvgURL); err != nil {
		metrics.IncRefreshFailure("write_m3u")
		metrics.RecordPlaylistFileValidity("m3u", false)
		err = fmt.Errorf("failed to write M3U playlist: %w", err)
		logJobError("refresh", logger.Error().Err(err).Str("event", "refresh.failed").Str("stage", "write_m3u").Str("path", playlistPath), err).Msg("failed to write M3U playlist")
		return err
	}

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

	return nil
}

func writeRefreshXMLTV(ctx context.Context, cfg config.AppConfig, rt config.RuntimeSnapshot, client epgFetchClient, items []playlist.Item) (int, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

	if cfg.XMLTVPath == "" {
		metrics.RecordXMLTV(false, 0, nil)
		metrics.RecordPlaylistFileValidity("xmltv", false) // XMLTV disabled
		return 0, nil
	}

	xmlCh := buildXMLTVChannels(ctx, items, rt.PublicURL)
	xmltvFullPath := filepath.Join(cfg.DataDir, cfg.XMLTVPath)
	allProgrammes := collectRefreshEPGProgrammes(ctx, cfg, client, items)

	var programmesForXMLTV []epg.Programme
	if cfg.EPGEnabled && len(allProgrammes) > 0 {
		programmesForXMLTV = allProgrammes
	}
	tv := epg.GenerateXMLTV(xmlCh, programmesForXMLTV)
	xmlErr := writeXMLTV(ctx, xmltvFullPath, tv)

	metrics.RecordXMLTV(true, len(xmlCh), xmlErr)
	if xmlErr != nil {
		metrics.IncRefreshFailure("xmltv")
		metrics.RecordPlaylistFileValidity("xmltv", false)
		xmlErr = fmt.Errorf("failed to write XMLTV file to %q: %w", xmltvFullPath, xmlErr)
		logJobError("refresh", logger.Error().Err(xmlErr).Str("event", "refresh.failed").Str("stage", "xmltv").Str("path", xmltvFullPath), xmlErr).Msg("failed to write XMLTV file")
		return 0, xmlErr
	}

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

	return len(allProgrammes), nil
}

func buildXMLTVChannels(ctx context.Context, items []playlist.Item, publicURL string) []epg.Channel {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

	xmlCh := make([]epg.Channel, 0, len(items))
	for _, it := range items {
		if len(xmlCh) < 5 {
			logger.Info().Str("channel", it.Name).Str("sref", it.ServiceRef).Msg("Debug: XMLTV Channel")
		}
		ch := epg.Channel{ID: it.ServiceRef, DisplayName: []string{it.Name}}
		if it.TvgLogo != "" {
			logo := it.TvgLogo
			if publicURL != "" && strings.HasPrefix(logo, "/") {
				logo = strings.TrimRight(publicURL, "/") + logo
			}
			ch.Icon = &epg.Icon{Src: logo}
		}
		xmlCh = append(xmlCh, ch)
	}
	return xmlCh
}

func collectRefreshEPGProgrammes(ctx context.Context, cfg config.AppConfig, client epgFetchClient, items []playlist.Item) []epg.Programme {
	if !cfg.EPGEnabled {
		return nil
	}

	logger := xglog.WithComponentFromContext(ctx, "jobs")
	logger.Info().
		Str("event", "epg.start").
		Int("channels", len(items)).
		Int("days", cfg.EPGDays).
		Msg("starting EPG collection")

	epgStartTime := time.Now()
	allProgrammes := collectEPGProgrammes(ctx, client, items, cfg)
	epgDuration := time.Since(epgStartTime).Seconds()

	if len(allProgrammes) == 0 {
		logger.Warn().
			Str("event", "epg.no_data").
			Msg("EPG collection returned no data")
	}

	channelsWithData := countProgrammeChannels(allProgrammes)
	metrics.RecordEPGCollection(len(allProgrammes), channelsWithData, epgDuration)

	logger.Info().
		Str("event", "epg.collected").
		Int("programmes", len(allProgrammes)).
		Int("channels_with_data", channelsWithData).
		Float64("duration_seconds", epgDuration).
		Msg("EPG collection completed")

	return allProgrammes
}

func countProgrammeChannels(programmes []epg.Programme) int {
	if len(programmes) == 0 {
		return 0
	}
	channels := make(map[string]bool)
	for _, prog := range programmes {
		channels[prog.Channel] = true
	}
	return len(channels)
}

func safeURLHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "<invalid-url>"
	}
	return u.Host
}
