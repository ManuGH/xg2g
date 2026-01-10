package read

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/m3u"
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
	LogoURL    string `json:"logo_url"`
	Number     string `json:"number"`
	Enabled    bool   `json:"enabled"`
	ServiceRef string `json:"service_ref"`
}

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

	// CTO Requirement: Return empty slice instead of nil for JSON consistency?
	// User Requirement (Parity): "If Slice 5.x is truly legacy parity, change to return nil."
	if len(bouquets) == 0 {
		return nil, nil
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
	if len(bouquets) == 0 {
		return nil
	}
	return bouquets
}

// GetServices returns a list of services filtered by bouquet.
func GetServices(cfg config.AppConfig, snap config.Snapshot, source ServicesSource, q ServicesQuery) (ServicesResult, error) {
	playlistName := snap.Runtime.PlaylistFilename
	if playlistName == "" {
		// No playlist configured -> logic was "no services found" -> null?
		// User says: "In the legacy code you included, an empty filename... os.ReadFile(dir) fails and legacy encodes [], not null."
		// So we must return EmptyEncodingArray.
		return ServicesResult{EmptyEncoding: EmptyEncodingArray}, nil
	}
	path := filepath.Clean(filepath.Join(cfg.DataDir, playlistName))

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("DEBUG: GetServices ReadFile failed: %s, err=%v\n", path, err)
		if os.IsNotExist(err) {
			// Playlist missing -> Legacy returns []
			return ServicesResult{EmptyEncoding: EmptyEncodingArray}, nil
		}
		// Read failure (permissions etc) -> Legacy returns []?
		// User said: "If os.ReadFile(path) fails -> it encodes []Service{} (JSON []), not null."
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

		// Extract service_ref from URL for streaming
		serviceRef := extractServiceRef(ch.URL, id)

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

func extractServiceRef(rawURL string, id string) string {
	serviceRef := ""
	if rawURL != "" {
		if u, err := url.Parse(rawURL); err == nil {
			// Check query params first (e.g. stream.m3u?ref=...)
			if ref := u.Query().Get("ref"); ref != "" {
				serviceRef = ref
			} else {
				// Fallback to path logic
				parts := strings.Split(u.Path, "/")
				if len(parts) > 0 {
					serviceRef = parts[len(parts)-1]
				}
			}
		} else {
			// Fallback for non-parseable URLs
			parts := strings.Split(rawURL, "/")
			if len(parts) > 0 {
				serviceRef = parts[len(parts)-1]
			}
		}
	}
	// Fallback to TvgID if service_ref not found
	if serviceRef == "" {
		serviceRef = id
	}
	return serviceRef
}
