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

	// 1) Items mit Picons bauen
	items := make([]playlist.Item, 0, len(svcs))
	for i, s := range svcs {
		name, sref := s[0], s[1]
		
		// Picon-URL automatisch generieren
		logo := ""
		if cfg.PiconBase != "" {
			logo = strings.TrimRight(cfg.PiconBase, "/") + "/" + url.PathEscape(sref) + ".png"
		} else {
			logo = openwebif.PiconURL(cfg.OWIBase, sref)
		}
		
		items = append(items, playlist.Item{
			Name:    name,
			TvgID:   "",
			TvgChNo: i + 1,
			URL:     openwebif.StreamURL(cfg.OWIBase, sref),
			Group:   cfg.Bouquet,
			TvgLogo: logo,
		})
	}

	// 2) XMLTV-Mapping: exakt + fuzzy
	if cfg.XMLTVPath != "" {
		if nameToID, err := epg.BuildNameToIDMap(cfg.XMLTVPath); err == nil {
			for i := range items {
				if cfg.FuzzyMax > 0 {
					// Fuzzy Matching aktiviert
					if id, ok := epg.FindBest(items[i].Name, nameToID, cfg.FuzzyMax); ok {
						items[i].TvgID = id
					}
				} else {
					// Nur exaktes Matching
					key := epg.NameKey(items[i].Name)
					if id, ok := nameToID[key]; ok {
						items[i].TvgID = id
					}
				}
			}
		}
	}

	// 3) M3U schreiben
	_ = os.MkdirAll(cfg.DataDir, 0o755)
	out := filepath.Join(cfg.DataDir, "playlist.m3u")
	f, err := os.Create(out)
	if err != nil {
		return nil, fmt.Errorf("create m3u: %w", err)
	}
	defer f.Close()

	if err := playlist.WriteM3U(f, items); err != nil {
		return nil, fmt.Errorf("write m3u: %w", err)
	}

	return &Status{
		LastRun:  time.Now(),
		Channels: len(items),
	}, nil
}
