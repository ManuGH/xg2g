package read

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/m3u"
)

// GetBouquets returns a deduplicated and sorted list of channel groups (bouquets).
func GetBouquets(cfg config.AppConfig, snap config.Snapshot) ([]string, error) {
	playlistName := snap.Runtime.PlaylistFilename
	if playlistName == "" {
		return getFallbackBouquets(cfg), nil
	}
	path := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	var bouquets []string
	seen := make(map[string]bool)

	data, err := os.ReadFile(path)
	if err == nil {
		channels := m3u.Parse(string(data))
		for _, ch := range channels {
			if ch.Group != "" && !seen[ch.Group] {
				bouquets = append(bouquets, ch.Group)
				seen[ch.Group] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Fallback to configured bouquets if none found in playlist (or playlist missing)
	if len(bouquets) == 0 {
		configured := strings.Split(cfg.Bouquet, ",")
		for _, b := range configured {
			if trimmed := strings.TrimSpace(b); trimmed != "" {
				if !seen[trimmed] {
					bouquets = append(bouquets, trimmed)
					seen[trimmed] = true
				}
			}
		}
	}

	// CTO Requirement: Deterministic ordering
	sort.Strings(bouquets)

	// CTO Requirement: Return empty slice instead of nil for JSON consistency
	if bouquets == nil {
		return []string{}, nil
	}

	return bouquets, nil
}

func getFallbackBouquets(cfg config.AppConfig) []string {
	var bouquets []string
	seen := make(map[string]bool)
	configured := strings.Split(cfg.Bouquet, ",")
	for _, b := range configured {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			if !seen[trimmed] {
				bouquets = append(bouquets, trimmed)
				seen[trimmed] = true
			}
		}
	}
	sort.Strings(bouquets)
	if bouquets == nil {
		return []string{}
	}
	return bouquets
}
