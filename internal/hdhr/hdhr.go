// SPDX-License-Identifier: MIT
package hdhr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/rs/zerolog"
)

// Config holds HDHomeRun emulation configuration
type Config struct {
	Enabled      bool
	DeviceID     string
	FriendlyName string
	ModelName    string
	FirmwareName string
	BaseURL      string
	TunerCount   int
	Logger       zerolog.Logger
}

// Server implements HDHomeRun API endpoints
type Server struct {
	config Config
	logger zerolog.Logger
}

// NewServer creates a new HDHomeRun emulation server
func NewServer(config Config) *Server {
	// Generate device ID if not provided
	if config.DeviceID == "" {
		config.DeviceID = "XG2G1234"
	}

	// Set defaults
	if config.FriendlyName == "" {
		config.FriendlyName = "xg2g"
	}
	if config.ModelName == "" {
		config.ModelName = "HDHR-xg2g"
	}
	if config.FirmwareName == "" {
		config.FirmwareName = "xg2g-1.4.0"
	}
	if config.TunerCount == 0 {
		config.TunerCount = 4
	}

	return &Server{
		config: config,
		logger: config.Logger,
	}
}

// DiscoverResponse represents HDHomeRun discovery response
type DiscoverResponse struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	DeviceAuth      string `json:"DeviceAuth"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// LineupStatus represents tuner status
type LineupStatus struct {
	ScanInProgress int    `json:"ScanInProgress"`
	ScanPossible   int    `json:"ScanPossible"`
	Source         string `json:"Source"`
	SourceList     []string `json:"SourceList"`
}

// LineupEntry represents a channel in the lineup
type LineupEntry struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
}

// HandleDiscover handles /discover.json endpoint
func (s *Server) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	baseURL := s.config.BaseURL
	if baseURL == "" {
		// Try to determine base URL from request
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, r.Host)
	}

	response := DiscoverResponse{
		FriendlyName:    s.config.FriendlyName,
		ModelNumber:     s.config.ModelName,
		FirmwareName:    s.config.FirmwareName,
		FirmwareVersion: s.config.FirmwareName,
		DeviceID:        s.config.DeviceID,
		DeviceAuth:      "xg2g",
		BaseURL:         baseURL,
		LineupURL:       fmt.Sprintf("%s/lineup.json", baseURL),
		TunerCount:      s.config.TunerCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	s.logger.Info().
		Str("endpoint", "/discover.json").
		Str("device_id", s.config.DeviceID).
		Msg("HDHomeRun discovery request")
}

// HandleLineupStatus handles /lineup_status.json endpoint
func (s *Server) HandleLineupStatus(w http.ResponseWriter, r *http.Request) {
	response := LineupStatus{
		ScanInProgress: 0,
		ScanPossible:   1,
		Source:         "Antenna",
		SourceList:     []string{"Antenna"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	s.logger.Debug().
		Str("endpoint", "/lineup_status.json").
		Msg("HDHomeRun lineup status request")
}

// HandleLineup handles /lineup.json endpoint
// This needs to be implemented to return actual channels
func (s *Server) HandleLineup(w http.ResponseWriter, r *http.Request) {
	// This will be populated by the main API server with actual channels
	// For now, return empty array
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode([]LineupEntry{})

	s.logger.Debug().
		Str("endpoint", "/lineup.json").
		Msg("HDHomeRun lineup request")
}

// HandleLineupPost handles POST /lineup.json (Plex scan)
func (s *Server) HandleLineupPost(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("scan")

	if action == "start" {
		s.logger.Info().Msg("HDHomeRun channel scan started (simulated)")
	}

	w.WriteHeader(http.StatusNoContent)
}

// GetConfigFromEnv creates Config from environment variables
func GetConfigFromEnv(logger zerolog.Logger) Config {
	enabled := strings.ToLower(os.Getenv("XG2G_HDHR_ENABLED")) == "true"

	return Config{
		Enabled:      enabled,
		DeviceID:     os.Getenv("XG2G_HDHR_DEVICE_ID"),
		FriendlyName: getEnvDefault("XG2G_HDHR_FRIENDLY_NAME", "xg2g"),
		ModelName:    getEnvDefault("XG2G_HDHR_MODEL", "HDHR-xg2g"),
		FirmwareName: getEnvDefault("XG2G_HDHR_FIRMWARE", "xg2g-1.4.0"),
		BaseURL:      os.Getenv("XG2G_HDHR_BASE_URL"),
		TunerCount:   getEnvInt("XG2G_HDHR_TUNER_COUNT", 4),
		Logger:       logger,
	}
}

func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var i int
		if _, err := fmt.Sscanf(value, "%d", &i); err == nil {
			return i
		}
	}
	return defaultValue
}
