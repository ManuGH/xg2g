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
    epg "github.com/ManuGH/xg2g/internal/epg"
    "github.com/ManuGH/xg2g/internal/openwebif"
    "github.com/ManuGH/xg2g/internal/playlist"
)
package jobs
import (
package jobs
import (
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
		return nil, err
	}
	ref, ok := bqs[cfg.Bouquet]
	if !ok {
		return nil, fmt.Errorf("bouquet not found: %s", cfg.Bouquet)
	}
	svcs, err := cl.Services(ctx, ref)
	if err != nil {
		return nil, err
	}

	items := make([]playlist.Item, 0, len(svcs))
	for i, s := range svcs {
		name, sref := s[0], s[1]
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
			TvgLogo: logo,
			Group:   cfg.Bouquet,
			URL:     openwebif.StreamURL(cfg.OWIBase, sref),
		})
	}

	if cfg.XMLTVPath != "" {
		if nameToID, err := epg.BuildNameToIDMap(cfg.XMLTVPath); err == nil {
			for i := range items {
				if cfg.FuzzyMax > 0 {
					if id, ok := epg.FindBest(items[i].Name, nameToID, cfg.FuzzyMax); ok {
						items[i].TvgID = id
					}
				} else {
					if id, ok := nameToID[epg.NameKey(items[i].Name)]; ok {
						items[i].TvgID = id
					}
				}
			}
		}
	}

	_ = os.MkdirAll(cfg.DataDir, 0o755)
	tmp := filepath.Join(cfg.DataDir, ".playlist.tmp")
	out := filepath.Join(cfg.DataDir, "playlist.m3u")

	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	if err := playlist.WriteM3U(f, items); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	_ = f.Close()
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}

	return &Status{LastRun: time.Now(), Channels: len(items)}, nil
}
// NEU: XMLTV generieren (nur Channel-Liste)
if cfg.XMLTVPath != "" {
    xmlCh := make([]epg.Channel, 0, len(items))
    for _, it := range items {
        if it.TvgID != "" {
            ch := epg.Channel{ID: it.TvgID, DisplayName: []string{it.Name}}
            if it.TvgLogo != "" {
                ch.Icon = &epg.Icon{Src: it.TvgLogo}
            }
            xmlCh = append(xmlCh, ch)
        }
    }
    if err := epg.WriteXMLTV(xmlCh, cfg.XMLTVPath); err != nil {
        log.Printf("WARN: XMLTV generation failed: %v", err)
    } else {
        log.Printf("XMLTV generated at %s (%d channels)", cfg.XMLTVPath, len(xmlCh))
    }
}

	// NEU: XMLTV generieren (nur Channel-Liste)
	if cfg.XMLTVPath != "" {
		xmlCh := make([]epg.Channel, 0, len(items))
		for _, it := range items {
			if it.TvgID != "" {
				ch := epg.Channel{ID: it.TvgID, DisplayName: []string{it.Name}}
				if it.TvgLogo != "" { ch.Icon = &epg.Icon{Src: it.TvgLogo} }
				xmlCh = append(xmlCh, ch)
			}
		}
		if err := epg.WriteXMLTV(xmlCh, cfg.XMLTVPath); err != nil {
			log.Printf("WARN: XMLTV generation failed: %v", err)
		} else {
			log.Printf("XMLTV generated at %s (%d channels)", cfg.XMLTVPath, len(xmlCh))
		}
	}
