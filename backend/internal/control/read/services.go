package read

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/m3u"
	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// ServicesSource defines the interface needed to fetch service metadata.
type ServicesSource interface {
	IsEnabled(id string) bool
}

// ServicesQuery defines filtering parameters for services.
type ServicesQuery struct {
	Bouquet string
}

type EmptyEncoding int

const (
	EmptyEncodingNull  EmptyEncoding = iota // Default: return null
	EmptyEncodingArray                      // Return []
)

// ServicesResult wraps the service list with encoding semantics.
// When Items is empty, EmptyEncoding determines whether to return null (EmptyEncodingNull)
// or [] (EmptyEncodingArray) to strictly match legacy behavior (e.g. read failure vs empty list).
type ServicesResult struct {
	Items         []Service
	EmptyEncoding EmptyEncoding
}

// Service is a control-layer representation of a channel/service.
type Service struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Group      string `json:"group"`
	LogoURL    string `json:"logoUrl"`
	Number     string `json:"number"`
	Enabled    bool   `json:"enabled"`
	ServiceRef string `json:"serviceRef"`
}


// BouquetWithCount represents a named bouquet with a service count.
type BouquetWithCount struct {
	Name  string
	Count int
}

// GetBouquetsWithCounts returns bouquets with their service counts.
// Logic:
// - Parses M3U playlist (Single Pass).
// - Groups by `Group` title (case-insensitive key, first-seen casing for display).
// - Ignores entries with empty group.
// - Counts ENTRIES (playlist order, duplicates included).
// - RETURNS:
//   - Success: []BouquetWithCount (ordered by playlist appearance), fallback=false.
//   - Success (Empty File): []BouquetWithCount{}, fallback=false. (Truth: File exists, 0 bouquets).
//   - Error: os.ErrNotExist -> Fallback to Config strings (Count=0), fallback=true.
//   - Error: Other -> Return error (Fail Closed).
func GetBouquetsWithCounts(cfg config.AppConfig, snap config.Snapshot) ([]BouquetWithCount, bool, error) {
	playlistName := strings.TrimSpace(snap.Runtime.PlaylistFilename)
	if playlistName == "" {
		return getFallbackBouquetsWithCounts(cfg), true, nil
	}
	path, err := paths.ValidatePlaylistPath(cfg.DataDir, playlistName)
	if err != nil {
		if os.IsNotExist(err) {
			return getFallbackBouquetsWithCounts(cfg), true, nil
		}
		return nil, false, err
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			// 2. Fallback if file not found
			return getFallbackBouquetsWithCounts(cfg), true, nil
		}
		// 3. Fail Closed on Read Error (Permissions, etc)
		return nil, false, err
	}

	// 4. Parse & Count
	channels := m3u.Parse(string(data))

	// Aggregation State
	counts := make(map[string]int)          // Key -> Count
	displayNames := make(map[string]string) // Key -> Display Name (First Seen)
	var order []string                      // Keys in order of appearance

	for _, ch := range channels {
		group := strings.TrimSpace(ch.Group)
		if group == "" {
			continue // Ignore empty groups
		}

		key := strings.ToLower(group) // Case-insensitive key

		if _, exists := counts[key]; !exists {
			counts[key] = 0
			displayNames[key] = group // Preserve first-seen casing
			order = append(order, key)
		}
		counts[key]++
	}

	// 5. Build Result (Preserve Order)
	result := make([]BouquetWithCount, 0, len(order))
	for _, key := range order {
		result = append(result, BouquetWithCount{
			Name:  displayNames[key],
			Count: counts[key],
		})
	}

	// 6. Truthfulness: If the playlist file exists but has no groups (len=0),
	// return empty list without falling back to config. Fallback to config only applies on ErrNotExist.
	return result, false, nil

}

func getFallbackBouquetsWithCounts(cfg config.AppConfig) []BouquetWithCount {
	var result []BouquetWithCount
	seen := make(map[string]bool)
	configured := strings.Split(cfg.Bouquet, ",")
	for _, b := range configured {
		if trimmed := strings.TrimSpace(b); trimmed != "" {
			key := strings.ToLower(trimmed)
			if !seen[key] {
				result = append(result, BouquetWithCount{
					Name:  trimmed,
					Count: 0, // Truth: Config fallback has unknown/zero services
				})
				seen[key] = true
			}
		}
	}
	// Legacy fallback sorts strictly?
	// getFallbackBouquets sorts strings.
	// We should probably sort here too for determinism (Config order vs Alpha?).
	// User said: "Preserve config order" for fallback.
	return result
}

// GetServices returns a list of services filtered by bouquet.
func GetServices(cfg config.AppConfig, snap config.Snapshot, source ServicesSource, q ServicesQuery) (ServicesResult, error) {
	playlistName := strings.TrimSpace(snap.Runtime.PlaylistFilename)
	if playlistName == "" {
		return ServicesResult{EmptyEncoding: EmptyEncodingArray}, nil
	}
	path, err := paths.ValidatePlaylistPath(cfg.DataDir, playlistName)
	if err != nil {
		if os.IsNotExist(err) {
			return ServicesResult{EmptyEncoding: EmptyEncodingArray}, nil
		}
		return ServicesResult{}, err
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			// Playlist missing -> Legacy returns []
			return ServicesResult{EmptyEncoding: EmptyEncodingArray}, nil
		}
		// Read failure (permissions etc) -> Legacy returns []?
		// User said: "If os.ReadFile(filepath.Clean(path)) fails -> it encodes []Service{} (JSON []), not null."
		// So we strictly return EmptyEncodingArray and no error (unless it's a critical system error?)
		// Legacy swallowed read errors?
		// "if err == nil { ... } else { // do nothing, empty list }"
		// So we return empty result with Array encoding, and NO error to avoid 500.
		return ServicesResult{EmptyEncoding: EmptyEncodingArray}, nil
	}

	channels := m3u.Parse(string(data))
	var services []Service

	for _, ch := range channels {
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}

		if q.Bouquet != "" && ch.Group != q.Bouquet {
			continue
		}

		enabled := true
		if source != nil {
			enabled = source.IsEnabled(id)
		}

		name := ch.Name
		group := ch.Group
		logo := ch.Logo

		publicURL := snap.Runtime.PublicURL
		if publicURL != "" && strings.HasPrefix(logo, publicURL) {
			logo = strings.TrimPrefix(logo, publicURL)
		}
		number := ch.Number

		// Extract serviceRef from URL for streaming
		serviceRef := ExtractServiceRef(ch.URL, id)

		// Rewrite Logo to use local proxy (avoids mixed content & external reachability issues)
		if serviceRef != "" {
			piconRef := strings.ReplaceAll(serviceRef, ":", "_")
			piconRef = strings.TrimSuffix(piconRef, "_")
			// Use relative path so frontend resolves to correct host
			logo = fmt.Sprintf("/logos/%s.png", piconRef)
		}

		services = append(services, Service{
			ID:         id,
			Name:       name,
			Group:      group,
			LogoURL:    logo,
			Number:     number,
			Enabled:    enabled,
			ServiceRef: serviceRef,
		})
	}

	// If we found 0 services (and read was successful), legacy behavior: usually null (if initialized as nil)
	// We return default EmptyEncodingNull.
	return ServicesResult{Items: services}, nil
}
