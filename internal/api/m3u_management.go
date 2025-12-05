package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/m3u"
)

// handleAPIPlaylistDownload serves the M3U playlist as a download
func (s *Server) handleAPIPlaylistDownload(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}

	path, err := s.dataFilePath(playlistName)
	if err != nil {
		logger.Error().Err(err).Msg("failed to resolve playlist path")
		http.Error(w, "Playlist not found", http.StatusNotFound)
		return
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		logger.Error().Err(err).Msg("failed to read playlist")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Parse and filter
	channels := m3u.Parse(string(content))
	var filtered []m3u.Channel
	for _, ch := range channels {
		id := ch.TvgID
		if id == "" {
			id = ch.Name
		}
		if s.channelManager == nil || s.channelManager.IsEnabled(id) {
			filtered = append(filtered, ch)
		}
	}

	// Generate filtered M3U
	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	for _, ch := range filtered {
		sb.WriteString(ch.Raw + "\n")
		sb.WriteString(ch.URL + "\n")
	}

	w.Header().Set("Content-Disposition", "attachment; filename="+playlistName)
	w.Header().Set("Content-Type", "audio/x-mpegurl")
	_, _ = w.Write([]byte(sb.String()))
}

// handleAPIXMLTVDownload serves the XMLTV file as a download
func (s *Server) handleAPIXMLTVDownload(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	if s.cfg.XMLTVPath == "" {
		http.Error(w, "XMLTV not configured", http.StatusNotFound)
		return
	}

	path, err := s.dataFilePath(s.cfg.XMLTVPath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to resolve XMLTV path")
		http.Error(w, "XMLTV not found", http.StatusNotFound)
		return
	}

	filename := filepath.Base(path)
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.Header().Set("Content-Type", "application/xml")
	http.ServeFile(w, r, path)
}

// handleAPIRegenerate triggers a playlist/EPG refresh
func (s *Server) handleAPIRegenerate(w http.ResponseWriter, r *http.Request) {
	// Reuse existing refresh logic
	s.handleRefresh(w, r)
}

// handleAPIFileStatus returns status of M3U and XMLTV files
func (s *Server) handleAPIFileStatus(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "api")

	status := struct {
		M3U   fileInfo `json:"m3u"`
		XMLTV fileInfo `json:"xmltv"`
	}{}

	// Check M3U
	playlistName := os.Getenv("XG2G_PLAYLIST_FILENAME")
	if strings.TrimSpace(playlistName) == "" {
		playlistName = "playlist.m3u"
	}
	if path, err := s.dataFilePath(playlistName); err == nil {
		if info, err := os.Stat(path); err == nil {
			status.M3U.Exists = true
			status.M3U.Size = info.Size()
			status.M3U.LastModified = info.ModTime().Format("2006-01-02T15:04:05Z07:00")
		}
	}

	// Check XMLTV
	if s.cfg.XMLTVPath != "" {
		if path, err := s.dataFilePath(s.cfg.XMLTVPath); err == nil {
			if info, err := os.Stat(path); err == nil {
				status.XMLTV.Exists = true
				status.XMLTV.Size = info.Size()
				status.XMLTV.LastModified = info.ModTime().Format("2006-01-02T15:04:05Z07:00")
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		logger.Error().Err(err).Msg("failed to encode file status")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

type fileInfo struct {
	Exists       bool   `json:"exists"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}
