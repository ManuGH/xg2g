package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.healthManager.ServeHealth(w, r)
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.healthManager.ServeReady(w, r)
}

func (s *Server) handleXMLTV(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	if strings.TrimSpace(s.cfg.XMLTVPath) == "" {
		logger.Warn().Str("event", "xmltv.not_configured").Msg("XMLTV path not configured")
		http.Error(w, "XMLTV file not available", http.StatusNotFound)
		return
	}

	// Get XMLTV file path with traversal protection
	xmltvPath, err := s.dataFilePath(s.cfg.XMLTVPath)
	if err != nil {
		logger.Error().Err(err).Str("event", "xmltv.invalid_path").Msg("XMLTV path rejected")
		http.Error(w, "XMLTV file not available", http.StatusNotFound)
		return
	}

	// Check if file exists
	fileInfo, err := os.Stat(xmltvPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn().
				Str("event", "xmltv.not_found").
				Str("path", xmltvPath).
				Msg("XMLTV file not found")
			http.Error(w, "XMLTV file not available", http.StatusNotFound)
			return
		}
		logger.Error().Err(err).Msg("failed to stat XMLTV file")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Security: Limit file size to prevent memory exhaustion (50MB max)
	const maxFileSize = 50 * 1024 * 1024
	if fileInfo.Size() > maxFileSize {
		logger.Warn().
			Int64("size", fileInfo.Size()).
			Str("event", "xmltv.too_large").
			Msg("XMLTV file exceeds maximum size")
		http.Error(w, "XMLTV file too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Read XMLTV file
	// #nosec G304 -- xmltvPath is validated by dataFilePath and confined to the data directory
	xmltvData, err := os.ReadFile(xmltvPath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to read XMLTV file")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Read M3U to build tvg-id to tvg-chno mapping
	m3uPath, err := s.dataFilePath(s.snap.Runtime.PlaylistFilename)
	if err != nil {
		logger.Warn().Err(err).Msg("playlist path rejected, serving raw XMLTV")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if _, writeErr := w.Write(xmltvData); writeErr != nil {
			logger.Error().Err(writeErr).Msg("failed to write raw XMLTV response")
		}
		return
	}

	// #nosec G304 -- m3uPath is validated by dataFilePath and confined to the data directory
	data, err := os.ReadFile(m3uPath)
	if err != nil {
		logger.Warn().Err(err).Str("path", m3uPath).Msg("failed to read playlist for XMLTV mapping")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(xmltvData)
		return
	}

	idToNumber := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			var tvgID, tvgChno string

			// Extract tvg-id
			if idx := strings.Index(line, `tvg-id="`); idx != -1 {
				start := idx + 8
				if end := strings.Index(line[start:], `"`); end != -1 {
					tvgID = line[start : start+end]
				}
			}

			// Extract tvg-chno
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				start := idx + 10
				if end := strings.Index(line[start:], `"`); end != -1 {
					tvgChno = line[start : start+end]
				}
			}

			if tvgID != "" && tvgChno != "" {
				idToNumber[tvgID] = tvgChno
			}
		}
	}

	// Replace all channel IDs in XMLTV
	xmltvString := string(xmltvData)
	for oldID, newID := range idToNumber {
		// Replace in channel elements: <channel id="sref-...">
		xmltvString = strings.ReplaceAll(xmltvString, `id="`+oldID+`"`, `id="`+newID+`"`)
		// Replace in programme elements: <programme channel="sref-...">
		xmltvString = strings.ReplaceAll(xmltvString, `channel="`+oldID+`"`, `channel="`+newID+`"`)
	}

	// Serve the modified XMLTV
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300") // Cache for 5 minutes
	if _, err := w.Write([]byte(xmltvString)); err != nil {
		logger.Error().Err(err).Msg("failed to write XMLTV response")
		return
	}

	logger.Debug().
		Str("event", "xmltv.served").
		Str("path", xmltvPath).
		Int("mappings", len(idToNumber)).
		Msg("XMLTV file served with channel ID remapping")
}

// Validation Request Model
type setupValidateRequest struct {
	BaseURL  string `json:"baseUrl"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupValidateResponse struct {
	Valid    bool                 `json:"valid"`
	Message  string               `json:"message"`
	Bouquets []string             `json:"bouquets,omitempty"`
	Version  *openwebif.AboutInfo `json:"version,omitempty"`
}

// handleSetupValidate implements the validation endpoint
func (s *Server) handleSetupValidate(w http.ResponseWriter, r *http.Request) {
	var req setupValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.BaseURL == "" {
		http.Error(w, "Base URL is required", http.StatusBadRequest)
		return
	}

	// Smart Fix: Handle missing scheme (lazy user input)
	if !strings.HasPrefix(req.BaseURL, "http://") && !strings.HasPrefix(req.BaseURL, "https://") {
		req.BaseURL = "http://" + req.BaseURL
	}

	// Create ephemeral client
	client := openwebif.NewWithPort(req.BaseURL, 0, openwebif.Options{
		Timeout:  5 * time.Second, // Fast timeout for validation
		Username: req.Username,
		Password: req.Password,
	})

	// 1. Check Connectivity (Get About Info)
	about, err := client.About(r.Context())
	if err != nil {
		// Log detailed error but return generic message to UI (or detailed if safe)
		log.L().Warn().Err(err).Str("baseUrl", req.BaseURL).Msg("validation failed: connection error")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(setupValidateResponse{
			Valid:   false,
			Message: fmt.Sprintf("Connection failed: %s", err.Error()),
		})
		return
	}

	// 2. Fetch Bouquets (Metadata)
	bouquetsMap, err := client.Bouquets(r.Context())
	if err != nil {
		log.L().Warn().Err(err).Msg("validation warning: could not fetch bouquets")
	}

	bouquetsList := make([]string, 0, len(bouquetsMap))
	for name := range bouquetsMap {
		bouquetsList = append(bouquetsList, name)
	}
	sort.Strings(bouquetsList)

	// Success
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(setupValidateResponse{
		Valid:    true,
		Message:  "Connection successful",
		Version:  about,
		Bouquets: bouquetsList,
	})
}
