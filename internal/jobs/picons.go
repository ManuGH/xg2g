package jobs

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/openwebif"
	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/rs/zerolog/log"
)

// PrewarmPicons downloads all missing picons in the background
func PrewarmPicons(ctx context.Context, cfg config.AppConfig, items []playlist.Item) {
	log.Info().Int("count", len(items)).Msg("Picon: Starting background pre-warm")

	piconDir := filepath.Join(cfg.DataDir, "picons")
	if err := os.MkdirAll(piconDir, 0755); err != nil {
		log.Error().Err(err).Msg("Picon: failed to create cache dir")
		return
	}

	upstreamBase := cfg.PiconBase
	if upstreamBase == "" {
		upstreamBase = cfg.OWIBase
	}

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	// Extract refs and dedup
	refs := make(map[string]bool)
	for _, item := range items {
		// TvgLogo is "/logos/1_0_19..._0_0_0.png?v=..."
		if item.TvgLogo == "" {
			continue
		}

		// Parse Logo URL to Ref
		// "/logos/REF.png?v=..."
		parts := strings.Split(item.TvgLogo, "/")
		if len(parts) == 0 {
			continue
		}
		filename := parts[len(parts)-1] // "REF.png?v=123"
		if idx := strings.Index(filename, "?"); idx != -1 {
			filename = filename[:idx]
		}
		if idx := strings.Index(filename, ".png"); idx != -1 {
			refUnderscore := filename[:idx]
			// Convert Underscore -> Colon for Upstream URL generation
			refColon := strings.ReplaceAll(refUnderscore, "_", ":")
			refs[refColon] = true
		}
	}

	log.Info().Int("unique_picons", len(refs)).Msg("Picon: Identified unique picons to warm")

	// Semaphore to limit concurrency (10 concurrent downloads)
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for ref := range refs {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			downloadPicon(ctx, client, upstreamBase, piconDir, r)
		}(ref)
	}

	wg.Wait()
	log.Info().Msg("Picon: Background pre-warm completed")
}

// downloadPicon downloads a single picon
func downloadPicon(ctx context.Context, client http.Client, upstreamBase, piconDir, ref string) {
	// Logic similar to http.go, but purely internal
	// ref here is "1:0:19..." (colon based)

	// Target Filename (Underscore based)
	storeRef := strings.ReplaceAll(ref, ":", "_")
	storeRef = strings.TrimRight(storeRef, "_") // Ensure clean filename
	localPath := filepath.Join(piconDir, storeRef+".png")

	if _, err := os.Stat(localPath); err == nil {
		return // Already exists
	}

	log.Debug().Str("ref", ref).Msg("Picon: Pre-warming...")

	// URL logic
	upstreamURL := openwebif.PiconURL(upstreamBase, ref)
	resp, err := client.Get(upstreamURL)

	// Fallback logic...
	if (err == nil && resp.StatusCode == http.StatusNotFound) || err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		normalized := openwebif.NormalizeServiceRefForPicon(ref)
		if normalized != ref {
			fallbackURL := openwebif.PiconURL(upstreamBase, normalized)
			respFallback, errFallback := client.Get(fallbackURL)
			if errFallback == nil && respFallback.StatusCode == http.StatusOK {
				resp = respFallback
				err = nil
			} else {
				if respFallback != nil {
					respFallback.Body.Close()
				}
			}
		}
	}

	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return // Skip failed
	}
	defer resp.Body.Close()

	// Save
	tempFile, err := os.CreateTemp(piconDir, "picon-warm-*.tmp")
	if err != nil {
		return
	}
	defer os.Remove(tempFile.Name())

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		tempFile.Close()
		return
	}
	tempFile.Close()
	os.Rename(tempFile.Name(), localPath)
}
