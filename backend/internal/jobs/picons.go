// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package jobs

import (
	"context"
	"strings"

	"github.com/ManuGH/xg2g/internal/playlist"
	"github.com/rs/zerolog/log"
)

// PrewarmPicons downloads all missing picons in the background
func PrewarmPicons(ctx context.Context, pool *PiconPool, items []playlist.Item) {
	if pool == nil {
		log.Debug().Msg("Picon: pre-warm skipped because no worker pool is configured")
		return
	}

	log.Info().Int("count", len(items)).Msg("Picon: Starting background pre-warm")

	refs := extractPiconRefs(items)
	log.Info().Int("unique_picons", len(refs)).Msg("Picon: Identified unique picons to warm")

	var enq, dropped int
	for ref := range refs {
		if ctx.Err() != nil {
			break
		}
		// Enqueue returns true if accepted (or handled via dedup/cache), false if dropped/error
		if pool.Enqueue(ctx, ref) {
			enq++
		} else {
			dropped++
		}
	}

	log.Info().
		Int("enqueued", enq).
		Int("dropped", dropped).
		Msg("Picon: pre-warm queued")
}

// extractPiconRefs parses unique picon refs from playlist items
func extractPiconRefs(items []playlist.Item) map[string]bool {
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
	return refs
}

// downloadPicon is deprecated/legacy; logic moved to PiconPool.downloadOne
// Keeping local helper removed to enforce pool usage.
