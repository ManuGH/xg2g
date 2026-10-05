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
	"github.com/ManuGH/xg2g/internal/epg/store"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	xglog "github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/platform/paths"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/ManuGH/xg2g/internal/telemetry"
	"github.com/ManuGH/xg2g/internal/validate"
	"github.com/rs/zerolog"
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
	piconPool       *PiconPool
	enrichmentStore store.EnrichmentStore
	enrichmentQueue *epg.EnrichmentQueue
	iptvParser      *sourceref.Parser
	iptvRegistry    *sourceref.Registry
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

func WithEnrichment(store store.EnrichmentStore, queue *epg.EnrichmentQueue) RefreshOption {
	return func(opts *refreshOptions) {
		opts.enrichmentStore = store
		opts.enrichmentQueue = queue
	}
}

func WithEnrichmentStore(store store.EnrichmentStore) RefreshOption {
	return func(opts *refreshOptions) {
		opts.enrichmentStore = store
	}
}

func WithEnrichmentQueue(queue *epg.EnrichmentQueue) RefreshOption {
	return func(opts *refreshOptions) {
		opts.enrichmentQueue = queue
	}
}

// WithIPTVSources provides an IPTV parser and registry to populate during refresh.
// If parser or reg is nil, IPTV source population is skipped.
func WithIPTVSources(parser *sourceref.Parser, reg *sourceref.Registry) RefreshOption {
	return func(opts *refreshOptions) {
		opts.iptvParser = parser
		opts.iptvRegistry = reg
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

	client := newRefreshClient(cfg, rt)

	bouquets, err := fetchRefreshBouquets(ctx, client, span)
	if err != nil {
		return nil, err
	}

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

	var proxyBase string
	if rt.UseProxyURLs {
		proxyBase = strings.TrimRight(rt.ProxyBaseURL, "/")
	}
	items, err := buildPlaylistItems(ctx, client, validBouquets, proxyBase)
	if err != nil {
		return nil, err
	}

	if refreshOpts.iptvParser != nil && refreshOpts.iptvRegistry != nil {
		populateIPTVRegistry(logger, refreshOpts.iptvParser, refreshOpts.iptvRegistry, items)
	}

	if err := writeRefreshPlaylist(ctx, cfg, rt, items, refreshOpts); err != nil {
		return nil, err
	}

	epgProgrammesCount, err := writeRefreshXMLTV(ctx, cfg, rt, client, items, refreshOpts)
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

// newRefreshClient builds the OpenWebIF client used for a refresh cycle from the
// configuration and runtime snapshots.
func newRefreshClient(cfg config.AppConfig, rt config.RuntimeSnapshot) *openwebif.Client {
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
	return openwebif.NewWithPort(cfg.Enigma2.BaseURL, cfg.Enigma2.StreamPort, owiOpts)
}

// fetchRefreshBouquets retrieves the available bouquets, recording the relevant
// tracing events, metrics, and span attributes. On failure it wraps the error and
// records it on the span.
func fetchRefreshBouquets(ctx context.Context, client *openwebif.Client, span trace.Span) (map[string]string, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

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

	return bouquets, nil
}

// buildPlaylistItems fetches the services for every resolved bouquet and turns them
// into playlist items, preserving bouquet ordering for sequential channel numbering.
// It records the per-bouquet service counts and the aggregate channel-type counts.
func buildPlaylistItems(ctx context.Context, client *openwebif.Client, validBouquets []resolvedBouquet, proxyBase string) ([]playlist.Item, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

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
			item, class, ok := buildPlaylistItem(ctx, client, s, bouquetName, channelNumber, proxyBase)
			if !ok {
				continue
			}

			// Naive channel type classification based on name/ref hints.
			switch class {
			case "radio":
				radio++
			case "hd":
				hd++
			case "sd":
				sd++
			default:
				unknown++
			}

			items = append(items, item)
			channelNumber++
		}
	}
	metrics.RecordChannelTypeCounts(hd, sd, radio, unknown)

	return items, nil
}

// buildPlaylistItem converts a single service entry into a playlist item, building
// and validating the stream URL, applying optional proxy rewriting, and computing the
// picon logo URL. It returns the item, its channel-type classification, and ok=false
// when the service should be skipped (stream URL build or validation failure).
func buildPlaylistItem(ctx context.Context, client *openwebif.Client, s [2]string, bouquetName string, channelNumber int, proxyBase string) (playlist.Item, string, bool) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

	name, ref := s[0], s[1]
	streamURL, err := client.StreamURL(ctx, ref, name)
	if err != nil {
		err = WrapStreamURLBuildError(fmt.Errorf("failed to build stream URL for %q: %w", name, err))
		logJobError("refresh", logger.Warn().Err(err).Str("service", name), err).Msg("failed to build stream URL")
		metrics.IncStreamURLBuild("failure")
		metrics.IncRefreshFailure("streamurl")
		return playlist.Item{}, "", false
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
		return playlist.Item{}, "", false
	}

	// If proxyBase is configured, rewrite URLs to point to xg2g proxy
	// This enables audio transcoding and smart stream detection for Plex/Jellyfin
	if proxyBase != "" {
		// Always rewrite using the known service reference (avoids dropping query params for WebIF URLs).
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
	var class string
	switch {
	case strings.Contains(lr, "radio") || strings.HasPrefix(ref, "1:0:2:"):
		class = "radio"
	case strings.Contains(lr, "hd"):
		class = "hd"
	case strings.Contains(lr, "sd"):
		class = "sd"
	default:
		class = "unknown"
	}

	item := playlist.Item{
		Name:       name,
		TvgID:      ref,           // Use ServiceRef as TvgID (raw numbers preferred by user)
		TvgChNo:    channelNumber, // Sequential numbering based on bouquet position
		TvgLogo:    logoURL,
		Group:      bouquetName, // Use actual bouquet name as group
		URL:        streamURL,
		ServiceRef: ref, // Explicitly store ref for EPG fetching
	}
	return item, class, true
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
		go PrewarmPicons(context.WithoutCancel(ctx), opts.piconPool, items)
	} else if cfg.PiconBase != "" {
		logger.Debug().Msg("skipping picon pre-warm because no runtime picon pool is configured")
	}

	// Pass Public URL to M3U writer for absolute paths in M3U (Plex compatibility).
	// WebUI uses relative paths internally.
	if err := writeM3U(ctx, playlistPath, items, rt.PublicURL, rt.XTvgURL, 0600); err != nil {
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

	// Generate and write masked public playlist (playlist_public.m3u) with 0644 permissions
	publicItems := buildPublicPlaylistItems(items, opts.iptvParser, rt.ProxyBaseURL, rt.PublicURL)
	publicPlaylistPath, err := paths.ValidatePlaylistPath(cfg.DataDir, "playlist_public.m3u")
	if err != nil {
		err = WrapPlaylistPathError(fmt.Errorf("invalid public playlist path: %w", err))
		logJobError("refresh", logger.Error().Err(err).Str("playlist", "playlist_public.m3u"), err).Msg("invalid public playlist path")
		metrics.IncRefreshFailure("playlist_path_invalid")
		return err
	}
	if err := writeM3U(ctx, publicPlaylistPath, publicItems, rt.PublicURL, rt.XTvgURL, 0644); err != nil {
		metrics.IncRefreshFailure("write_public_m3u")
		err = fmt.Errorf("failed to write public M3U playlist: %w", err)
		logJobError("refresh", logger.Error().Err(err).Str("event", "refresh.failed").Str("stage", "write_public_m3u").Str("path", publicPlaylistPath), err).Msg("failed to write public M3U playlist")
		return err
	}
	logger.Info().
		Str("event", "playlist_public.write").
		Str("path", publicPlaylistPath).
		Int("channels", len(publicItems)).
		Msg("public playlist written")

	return nil
}

// buildPublicPlaylistItems converts internal playlist items into public export playlist items.
// For IPTV services, it masks service references and URLs to opaque IDs (iptv_<id>) and
// routes live playback through /api/v3/stream/live/iptv_<id>, while masking logo URLs to
// /logos/iptv_<id>.png. Non-IPTV (DVB) items remain unaltered.
//
// Fail-closed contract: any IPTV service reference that cannot be converted to an opaque ID
// (e.g. parser is nil or parsing returns an error) is omitted from the public playlist export.
// It is NEVER copied into the public export in raw or fallback form.
func buildPublicPlaylistItems(items []playlist.Item, parser *sourceref.Parser, proxyBase, publicURL string) []playlist.Item {
	if len(items) == 0 {
		return nil
	}
	out := make([]playlist.Item, 0, len(items))
	for _, it := range items {
		ref := strings.TrimSpace(it.ServiceRef)
		if ref == "" {
			ref = strings.TrimSpace(it.TvgID)
		}

		if strings.HasPrefix(ref, sourceref.IDPrefix) {
			opaqueID := ref
			masked := it
			masked.TvgID = opaqueID
			masked.ServiceRef = opaqueID
			streamPath := "/api/v3/stream/live/" + opaqueID
			if proxyBase != "" {
				masked.URL = strings.TrimRight(proxyBase, "/") + streamPath
			} else if publicURL != "" {
				masked.URL = strings.TrimRight(publicURL, "/") + streamPath
			} else {
				masked.URL = streamPath
			}
			if it.TvgLogo != "" {
				query := ""
				if idx := strings.Index(it.TvgLogo, "?"); idx != -1 {
					query = it.TvgLogo[idx:]
				}
				masked.TvgLogo = fmt.Sprintf("/logos/%s.png%s", opaqueID, query)
			}
			out = append(out, masked)
			continue
		}

		parts := strings.Split(ref, ":")
		isIPTV := len(parts) > 0 && sourceref.IsIPTVServiceType(parts[0])

		if !isIPTV {
			out = append(out, it)
			continue
		}

		// Fail closed: if parser is missing, omit IPTV item from public export
		if parser == nil {
			continue
		}

		src, err := parser.Parse(ref)
		if err != nil {
			// Fail closed: if parsing fails, omit IPTV item from public export
			continue
		}

		opaqueID := string(src.ID())
		masked := it
		masked.TvgID = opaqueID
		masked.ServiceRef = opaqueID

		streamPath := "/api/v3/stream/live/" + opaqueID
		if proxyBase != "" {
			masked.URL = strings.TrimRight(proxyBase, "/") + streamPath
		} else if publicURL != "" {
			masked.URL = strings.TrimRight(publicURL, "/") + streamPath
		} else {
			masked.URL = streamPath
		}

		if it.TvgLogo != "" {
			query := ""
			if idx := strings.Index(it.TvgLogo, "?"); idx != -1 {
				query = it.TvgLogo[idx:]
			}
			masked.TvgLogo = fmt.Sprintf("/logos/%s.png%s", opaqueID, query)
		}

		out = append(out, masked)
	}
	return out
}

func writeRefreshXMLTV(ctx context.Context, cfg config.AppConfig, rt config.RuntimeSnapshot, client epgFetchClient, items []playlist.Item, opts refreshOptions) (int, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")

	if cfg.XMLTVPath == "" {
		metrics.RecordXMLTV(false, 0, nil)
		metrics.RecordPlaylistFileValidity("xmltv", false) // XMLTV disabled
		return 0, nil
	}

	xmlCh := buildXMLTVChannels(ctx, items, rt.PublicURL)
	xmltvFullPath := filepath.Join(cfg.DataDir, cfg.XMLTVPath)
	allProgrammes := collectRefreshEPGProgrammes(ctx, cfg, client, items, opts)

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

func collectRefreshEPGProgrammes(ctx context.Context, cfg config.AppConfig, client epgFetchClient, items []playlist.Item, opts refreshOptions) []epg.Programme {
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
	allProgrammes := collectEPGProgrammes(ctx, client, items, cfg, opts.enrichmentStore, opts.enrichmentQueue)
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

var testHookReplaceRegistry func(reg *sourceref.Registry, snapshot []sourceref.Source) error

func populateIPTVRegistry(logger zerolog.Logger, parser *sourceref.Parser, reg *sourceref.Registry, items []playlist.Item) {
	if parser == nil || reg == nil {
		return
	}
	var (
		parsedCount  int
		skippedCount int
		snapshot     []sourceref.Source
	)
	for _, item := range items {
		ref := strings.TrimSpace(item.ServiceRef)
		if ref == "" {
			continue
		}
		parts := strings.Split(ref, ":")
		if len(parts) == 0 || !sourceref.IsIPTVServiceType(parts[0]) {
			continue
		}
		src, err := parser.Parse(ref)
		if err != nil {
			skippedCount++
			continue
		}
		parsedCount++
		snapshot = append(snapshot, src)
	}

	replaceFn := reg.Replace
	if testHookReplaceRegistry != nil {
		replaceFn = func(snapshot []sourceref.Source) error {
			return testHookReplaceRegistry(reg, snapshot)
		}
	}

	if err := replaceFn(snapshot); err != nil {
		if errors.Is(err, sourceref.ErrCollision) {
			logger.Warn().Msg("iptv sources snapshot rejected due to collision; keeping previous snapshot")
			return
		}
		logger.Warn().Err(err).Msg("failed to update iptv sources registry")
		return
	}

	dedupedCount := parsedCount - reg.Len()
	logger.Info().
		Int("parsed", parsedCount).
		Int("skipped", skippedCount).
		Int("deduped", dedupedCount).
		Int("total_registered", reg.Len()).
		Msg("iptv sources registry updated")
}
