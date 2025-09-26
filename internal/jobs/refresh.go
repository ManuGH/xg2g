package jobs

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/epg"
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
	cl := openwebif.New(cfg.OWIBase)

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
			URL:     openwebif.StreamURL(cfg.OWIBase, sref),
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
			log.Printf("WARN: XMLTV generation failed: %v", err)
		} else {
			log.Printf("XMLTV generated at %s (%d channels)", cfg.XMLTVPath, len(xmlCh))
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
