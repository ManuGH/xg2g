package read

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// GetBouquets returns a deduplicated and sorted list of channel groups (bouquets).
func GetBouquets(cfg config.AppConfig, snap config.Snapshot) ([]string, error) {
	var bouquets []string
	seen := make(map[string]bool)

	playlistName := strings.TrimSpace(snap.Runtime.PlaylistFilename)
	if playlistName != "" {
		path, err := paths.ValidatePlaylistPath(cfg.DataDir, playlistName)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		} else {
			data, err := os.ReadFile(filepath.Clean(path))
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
		}
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

	// CTO Requirement: Return empty slice instead of nil for JSON consistency?
	// User Requirement (Parity): "If Slice 5.x is truly legacy parity, change to return nil."
	if len(bouquets) == 0 {
		return nil, nil
	}

	return bouquets, nil
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

	// 6. Handle Fallback if Playlist was Empty but Read Validly?
	// Legacy logic for GetBouquets falls back if len=0.
	// But User requires "Truthful". If M3U is valid but empty, result is [].
	// However, if NO groups found, do we fall back?
	// "Fallback to configured bouquets if none found in playlist"
	// The legacy GetBouquets does exactly that (lines 76+).
	// We should mirror that logic for consistency?
	// User Requirement: "Fallback only on NotExist" was the instruction for errors.
	// But what about efficient "empty playlist treated as fallback"?
	// Let's stick to "Fallback on NotExist".
	// If file exists and has no groups -> Return empty list (Truth).

	// WAIT. Legacy GetBouquets (lines 76-86) explicitly falls back if `len(bouquets) == 0`.
	// This happens if file exists but has no groups.
	// We should probably respect that "No Groups Found" -> Fallback Config?
	// But User said: "Fallback only on os.ErrNotExist... otherwise return error".
	// That was about ERRORS. What about EMPTY SUCCESS?
	// "If the underlying data source truly cannot provide per-bouquet contents... fallback to config count=0."
	// An empty playlist TRULY provides 0 contents.
	// But "Fallback to configured bouquets" implies we show bouquets that MIGHT be there?
	// Actually, if playlist is empty, showing config names with 0 count is honest.
	// Let's mirror the legacy "if len=0, try config" logic, it aligns with "System Availability".

	// 6. Strict Truthfulness:
	// If file existed but had no groups (len=0), we return empty list.
	// We DO NOT fall back to config here. That would be "inventing" bouquets when we know the file is empty.
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
		fmt.Printf("DEBUG: GetServices ReadFile failed: %s, err=%v\n", path, err)
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
