// SPDX-License-Identifier: MIT
package jobs

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/metrics"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
)

// ErrInvalidStreamPort marks an invalid stream port configuration.
var ErrInvalidStreamPort = errors.New("invalid stream port")

// Status represents the current state of the refresh job
type Status struct {
	Version  string    `json:"version"`
	LastRun  time.Time `json:"lastRun"`
	Channels int       `json:"channels"`
	Error    string    `json:"error,omitempty"`
}

// Config holds configuration for refresh operations
type Config struct {
	Version       string
	DataDir       string
	OWIBase       string
	Bouquet       string
	XMLTVPath     string
	PiconBase     string
	FuzzyMax      int
	StreamPort    int
	APIToken      string // Optional: for securing the /api/refresh endpoint
	OWITimeout    time.Duration
	OWIRetries    int
	OWIBackoff    time.Duration
	OWIMaxBackoff time.Duration
}

// Refresh performs the complete refresh cycle: fetch bouquets → services → write M3U + XMLTV
func Refresh(ctx context.Context, cfg Config) (*Status, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")
	logger.Info().Str("event", "refresh.start").Msg("starting refresh")

	if err := validateConfig(cfg); err != nil {
		metrics.IncConfigValidationError()
		metrics.IncRefreshFailure("config")
		return nil, err
	}

	opts := openwebif.Options{
		Timeout:    cfg.OWITimeout,
		MaxRetries: cfg.OWIRetries,
		Backoff:    cfg.OWIBackoff,
		MaxBackoff: cfg.OWIMaxBackoff,
	}
	client := openwebif.NewWithPort(cfg.OWIBase, cfg.StreamPort, opts)
	bouquets, err := client.Bouquets(ctx)
	if err != nil {
		metrics.IncRefreshFailure("bouquets")
		return nil, fmt.Errorf("failed to fetch bouquets: %w", err)
	}
	metrics.RecordBouquetsCount(len(bouquets))

	bouquetRef, ok := bouquets[cfg.Bouquet]
	if !ok {
		metrics.IncRefreshFailure("bouquets")
		return nil, fmt.Errorf("bouquet %q not found", cfg.Bouquet)
	}

	services, err := client.Services(ctx, bouquetRef)
	if err != nil {
		metrics.IncRefreshFailure("services")
		return nil, fmt.Errorf("failed to fetch services for bouquet %q: %w", cfg.Bouquet, err)
	}
	metrics.RecordServicesCount(cfg.Bouquet, len(services))

	items := make([]playlist.Item, 0, len(services))
	// Channel type counters for the last refresh
	hd, sd, radio, unknown := 0, 0, 0, 0
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
			TvgLogo: piconURL,
			Group:   cfg.Bouquet,
			URL:     streamURL,
		})
	}
	metrics.RecordChannelTypeCounts(hd, sd, radio, unknown)

	// Write M3U playlist (filename configurable via ENV)
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	playlistPath := filepath.Join(cfg.DataDir, playlistName)
	if err := writeM3U(ctx, playlistPath, items); err != nil {
		metrics.IncRefreshFailure("write_m3u")
		return nil, fmt.Errorf("failed to write M3U playlist: %w", err)
	}
	logger.Info().
		Str("event", "playlist.write").
		Str("path", playlistPath).
		Int("channels", len(items)).
		Msg("playlist written")

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
		xmlErr = epg.WriteXMLTV(xmlCh, xmltvFullPath)
		metrics.RecordXMLTV(true, len(xmlCh), xmlErr)
		if xmlErr != nil {
			metrics.IncRefreshFailure("xmltv")
			// Return the error to signal a failed job instead of just logging it.
			return nil, fmt.Errorf("failed to write XMLTV file to %q: %w", xmltvFullPath, xmlErr)
		}

		logger.Info().
			Str("event", "xmltv.success").
			Str("path", xmltvFullPath).
			Int("channels", len(xmlCh)).
			Msg("XMLTV generated")
	} else {
		metrics.RecordXMLTV(false, 0, nil)
	}

	status := &Status{LastRun: time.Now(), Channels: len(items)}
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
	sum := sha1.Sum([]byte(sref))
	return "sref-" + hex.EncodeToString(sum[:])
}

func validateConfig(cfg Config) error {
	if cfg.OWIBase == "" {
		return fmt.Errorf("openwebif base URL is empty")
	}

	u, err := url.Parse(cfg.OWIBase)
	if err != nil {
		return fmt.Errorf("invalid openwebif base URL %q: %w", cfg.OWIBase, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported openwebif base URL scheme %q", u.Scheme)
	}

	if u.Host == "" {
		return fmt.Errorf("openwebif base URL %q is missing host", cfg.OWIBase)
	}

	if cfg.StreamPort <= 0 || cfg.StreamPort > 65535 {
		return fmt.Errorf("%w: %d", ErrInvalidStreamPort, cfg.StreamPort)
	}

	// Validate DataDir to prevent path traversal attacks
	if cfg.DataDir == "" {
		return fmt.Errorf("data directory is empty")
	}

	// Convert to absolute path and validate
	absDataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("invalid data directory %q: %w", cfg.DataDir, err)
	}

	// Ensure the directory exists or can be created
	if err := os.MkdirAll(absDataDir, 0755); err != nil {
		return fmt.Errorf("cannot create data directory %q: %w", absDataDir, err)
	}

	// Check for directory traversal patterns
	if strings.Contains(cfg.DataDir, "..") {
		return fmt.Errorf("data directory %q contains path traversal sequences", cfg.DataDir)
	}

	return nil
}
