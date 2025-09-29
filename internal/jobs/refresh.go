// SPDX-License-Identifier: MIT
package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	xglog "github.com/ManuGH/xg2g/internal/log"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
)

// ErrInvalidStreamPort marks an invalid stream port configuration.
var ErrInvalidStreamPort = errors.New("invalid stream port")

// Status represents the current state of the refresh job
type Status struct {
	LastRun  time.Time `json:"lastRun"`
	Channels int       `json:"channels"`
	Error    string    `json:"error,omitempty"`
}

// Config holds configuration for refresh operations
type Config struct {
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
		return nil, fmt.Errorf("failed to fetch bouquets: %w", err)
	}

	bouquetRef, ok := bouquets[cfg.Bouquet]
	if !ok {
		return nil, fmt.Errorf("bouquet %q not found", cfg.Bouquet)
	}

	services, err := client.Services(ctx, bouquetRef)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch services for bouquet %q: %w", cfg.Bouquet, err)
	}

	items := make([]playlist.Item, 0, len(services))
	for _, s := range services {
		name, ref := s[0], s[1]
		streamURL, err := client.StreamURL(ref, name)
		if err != nil {
			logger.Warn().Err(err).Str("service", name).Msg("failed to build stream URL")
			continue
		}

		piconURL := ""
		if cfg.PiconBase != "" {
			piconURL = openwebif.PiconURL(cfg.PiconBase, ref)
		}

		items = append(items, playlist.Item{
			Name:    name,
			TvgID:   makeStableID(name),
			TvgLogo: piconURL,
			Group:   cfg.Bouquet,
			URL:     streamURL,
		})
	}

	// Write M3U playlist
	playlistPath := filepath.Join(cfg.DataDir, "playlist.m3u")
	if err := writeM3U(playlistPath, items); err != nil {
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
		if err := epg.WriteXMLTV(xmlCh, filepath.Join(cfg.DataDir, cfg.XMLTVPath)); err != nil {
			logger.Warn().
				Err(err).
				Str("event", "xmltv.failed").
				Str("path", cfg.XMLTVPath).
				Int("channels", len(xmlCh)).
				Msg("XMLTV generation failed")
		} else {
			logger.Info().
				Str("event", "xmltv.success").
				Str("path", cfg.XMLTVPath).
				Int("channels", len(xmlCh)).
				Msg("XMLTV generated")
		}
	}

	status := &Status{LastRun: time.Now(), Channels: len(items)}
	logger.Info().
		Str("event", "refresh.success").
		Int("channels", status.Channels).
		Msg("refresh completed")
	return status, nil
}

// writeM3U safely writes the playlist to a temporary file and renames it on success.
func writeM3U(path string, items []playlist.Item) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, "playlist-*.m3u.tmp")
	if err != nil {
		return fmt.Errorf("create temporary M3U file: %w", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name()) // Clean up temp file on error
	}()

	if err := playlist.WriteM3U(tmpFile, items); err != nil {
		return fmt.Errorf("write to temporary M3U file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary M3U file: %w", err)
	}

	// Atomically rename the temporary file to the final destination
	if err := os.Rename(tmpFile.Name(), path); err != nil {
		return fmt.Errorf("rename temporary M3U file: %w", err)
	}

	return nil
}

// makeStableID creates deterministic tvg-id from channel name
// Keep behavior stable to avoid breaking existing EPG mappings
func makeStableID(name string) string {
	// Normalize: lowercase, replace spaces/special chars with underscores
	id := strings.ToLower(name)
	id = strings.ReplaceAll(id, " ", "_")
	id = strings.ReplaceAll(id, ".", "_")
	id = strings.ReplaceAll(id, "-", "_")

	// Remove consecutive underscores
	for strings.Contains(id, "__") {
		id = strings.ReplaceAll(id, "__", "_")
	}

	// Trim leading/trailing underscores
	id = strings.Trim(id, "_")

	if id == "" {
		return "unknown"
	}
	return id
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
