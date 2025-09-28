package jobs

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
)

type Config struct {
	DataDir   string
	OWIBase   string
	Bouquet   string
	PiconBase string
	XMLTVPath string
	FuzzyMax  int
}

type Status struct {
	LastRun  time.Time
	Channels int
	Error    string
}

func Refresh(ctx context.Context, cfg Config) (*Status, error) {
	cfg.OWIBase = strings.TrimSpace(cfg.OWIBase)
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}

	cl := openwebif.New(cfg.OWIBase)
	return refreshWithClient(ctx, cfg, cl)
}

// refreshWithClient performs the refresh flow using the provided OpenWebIF client.
// Separated for easier testing.
func refreshWithClient(ctx context.Context, cfg Config, cl openwebif.ClientInterface) (*Status, error) {
	bqs, err := cl.Bouquets(ctx)
	if err != nil {
		return nil, fmt.Errorf("bouquets: %w", err)
	}
	ref, ok := bqs[cfg.Bouquet]
	if !ok {
		return nil, fmt.Errorf("bouquet not found: %s", cfg.Bouquet)
	}

	svcs, err := cl.Services(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("services: %w", err)
	}

	items := make([]playlist.Item, 0, len(svcs))
	for i, s := range svcs {
		if len(s) < 2 {
			continue
		}
		name, sref := s[0], s[1]

		logo := ""
		if cfg.PiconBase != "" {
			logo = strings.TrimRight(cfg.PiconBase, "/") + "/" + url.PathEscape(sref) + ".png"
		} else {
			logo = openwebif.PiconURL(cfg.OWIBase, sref)
		}

		items = append(items, playlist.Item{
			Name:    name,
			TvgID:   makeStableID(name),
			TvgChNo: i + 1,
			URL:     cl.StreamURL(sref),
			Group:   cfg.Bouquet,
			TvgLogo: logo,
		})
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir datadir: %w", err)
	}

	m3u := filepath.Join(cfg.DataDir, "playlist.m3u")
	f, err := os.Create(m3u)
	if err != nil {
		return nil, fmt.Errorf("create m3u: %w", err)
	}
	if err := playlist.WriteM3U(f, items); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write m3u: %w", err)
	}
	_ = f.Close()

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
			logger := log.WithComponentFromContext(ctx, "jobs")
			logger.Warn().
				Err(err).
				Str("event", "xmltv.failed").
				Str("path", cfg.XMLTVPath).
				Int("channels", len(xmlCh)).
				Msg("XMLTV generation failed")
		} else {
			logger := log.WithComponentFromContext(ctx, "jobs")
			logger.Info().
				Str("event", "xmltv.success").
				Str("path", cfg.XMLTVPath).
				Int("channels", len(xmlCh)).
				Msg("XMLTV generated")
		}
	}

	return &Status{LastRun: time.Now(), Channels: len(items)}, nil
}

func makeStableID(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	r := strings.NewReplacer(" ", ".", "/", ".", "\\", ".", "&", "and", "+", "plus")
	s = r.Replace(s)
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
			b.WriteRune(c)
		}
	}
	id := strings.Trim(b.String(), "._-")
	if id == "" {
		id = "ch"
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
