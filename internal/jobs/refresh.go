package jobs

import (
	"context"
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

// Status represents the current state of the refresh job
type Status struct {
	LastRun  time.Time `json:"last_run"`
	Channels int       `json:"channels"`
	Error    string    `json:"error,omitempty"`
}

// Config holds configuration for refresh operations
type Config struct {
	DataDir    string
	OWIBase    string
	Bouquet    string
	XMLTVPath  string
	PiconBase  string
	FuzzyMax   int
	StreamPort int
}

// Refresh performs the complete refresh cycle: fetch bouquets → services → write M3U + XMLTV
func Refresh(ctx context.Context, cfg Config) (*Status, error) {
	cfg.OWIBase = strings.TrimSpace(cfg.OWIBase)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	cl := openwebif.New(cfg.OWIBase)
	return refreshWithClient(ctx, cfg, cl)
}

// refreshWithClient is separated for easier testing
func refreshWithClient(ctx context.Context, cfg Config, cl openwebif.ClientInterface) (*Status, error) {
	logger := xglog.WithComponentFromContext(ctx, "jobs")
	logger.Info().Str("event", "refresh.start").Msg("starting refresh")

	bqs, err := cl.Bouquets(ctx)
	if err != nil {
		return nil, fmt.Errorf("bouquets: %w", err)
	}

	ref, ok := bqs[cfg.Bouquet]
	if !ok {
		return nil, fmt.Errorf("bouquet %q not found", cfg.Bouquet)
	}

	services, err := cl.Services(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("services for bouquet %q: %w", cfg.Bouquet, err)
	}

	items := make([]playlist.Item, 0, len(services))
	for i, svc := range services {
		if len(svc) < 2 {
			continue
		}
		name, sref := svc[0], svc[1]

		item := playlist.Item{
			Name:    name,
			TvgID:   makeStableID(name),
			TvgChNo: i + 1,
			Group:   cfg.Bouquet,
			URL:     cl.StreamURL(sref),
		}

		if cfg.PiconBase != "" {
			item.TvgLogo = strings.TrimRight(cfg.PiconBase, "/") + "/" + url.PathEscape(sref) + ".png"
		} else {
			item.TvgLogo = openwebif.PiconURL(cfg.OWIBase, sref)
		}

		items = append(items, item)
	}

	// Write M3U playlist
	playlistPath := filepath.Join(cfg.DataDir, "playlist.m3u")
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	f, err := os.Create(playlistPath)
	if err != nil {
		return nil, fmt.Errorf("create playlist: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			logger.Error().
				Err(cerr).
				Str("event", "playlist.close_error").
				Str("path", playlistPath).
				Msg("failed to close playlist file")
		}
	}()

	if err := playlist.WriteM3U(f, items); err != nil {
		return nil, fmt.Errorf("write playlist: %w", err)
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
		if err := epg.WriteXMLTV(xmlCh, cfg.XMLTVPath); err != nil {
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

	return nil
}
