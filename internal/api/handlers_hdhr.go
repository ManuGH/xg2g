package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/log"
)

// handleLineupJSON handles /lineup.json endpoint for HDHomeRun emulation
// It reads the M3U playlist and converts it to HDHomeRun lineup format
func (s *Server) handleLineupJSON(w http.ResponseWriter, r *http.Request) {
	logger := log.WithComponentFromContext(r.Context(), "hdhr")

	// Read the M3U playlist file
	m3uPath, err := s.dataFilePath(s.snap.Runtime.PlaylistFilename)
	if err != nil {
		logger.Error().Err(err).Str("event", "lineup.invalid_path").Msg("playlist path rejected")
		http.Error(w, "Lineup not available", http.StatusInternalServerError)
		return
	}

	// #nosec G304 -- m3uPath is validated by dataFilePath and confined to the data directory
	data, err := os.ReadFile(m3uPath)
	if err != nil {
		logger.Error().Err(err).Str("path", m3uPath).Msg("failed to read playlist file")
		http.Error(w, "Lineup not available", http.StatusInternalServerError)
		return
	}

	// Parse M3U content to extract channels
	var lineup []hdhr.LineupEntry
	lines := strings.Split(string(data), "\n")
	var currentChannel hdhr.LineupEntry
	forceHLS := s.hdhr != nil && s.hdhr.PlexForceHLS()

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			// Parse channel info from EXTINF line
			// Format: #EXTINF:-1 tvg-chno="X" tvg-id="sref-..." tvg-name="Channel Name",Display Name

			// Extract tvg-chno (channel number) - Plex uses this for EPG matching with XMLTV
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				start := idx + 10
				if end := strings.Index(line[start:], `"`); end != -1 {
					currentChannel.GuideNumber = line[start : start+end]
				}
			}

			// Extract channel name (after the last comma)
			if idx := strings.LastIndex(line, ","); idx != -1 {
				currentChannel.GuideName = strings.TrimSpace(line[idx+1:])
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") && currentChannel.GuideName != "" {
			// This is the stream URL
			streamURL := line
			if forceHLS {
				streamURL = addHLSProxyPrefix(streamURL)
			}

			currentChannel.URL = streamURL
			lineup = append(lineup, currentChannel)
			currentChannel = hdhr.LineupEntry{} // Reset for next channel
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(lineup); err != nil {
		logger.Error().Err(err).Msg("failed to encode lineup")
		return
	}

	logger.Debug().
		Int("channels", len(lineup)).
		Msg("HDHomeRun lineup served")
}

func addHLSProxyPrefix(raw string) string {
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.HasPrefix(parsed.Path, "/hls/") {
		return raw
	}
	trimmed := strings.TrimPrefix(parsed.Path, "/")
	if trimmed == "" {
		parsed.Path = "/hls"
	} else {
		parsed.Path = path.Join("/hls", trimmed)
	}
	return parsed.String()
}
