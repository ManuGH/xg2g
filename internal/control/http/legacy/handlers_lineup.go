// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package legacy

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/ManuGH/xg2g/internal/hdhr"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/platform/paths"
)

// HandleLineupJSON handles /lineup.json endpoint for HDHomeRun emulation.
func HandleLineupJSON(w http.ResponseWriter, r *http.Request, runtime Runtime) {
	logger := log.WithComponentFromContext(r.Context(), "hdhr")
	cfg := runtime.CurrentConfig()

	m3uPath, err := paths.ValidatePlaylistPath(cfg.DataDir, runtime.PlaylistFilename())
	if err != nil {
		logger.Error().Err(err).Str("event", "lineup.invalid_path").Msg("playlist path rejected")
		http.Error(w, "Lineup not available", http.StatusInternalServerError)
		return
	}

	// #nosec G304 -- m3uPath is validated and confined to the data directory
	data, err := os.ReadFile(m3uPath)
	if err != nil {
		logger.Error().Err(err).Str("path", m3uPath).Msg("failed to read playlist file")
		http.Error(w, "Lineup not available", http.StatusInternalServerError)
		return
	}

	var lineup []hdhr.LineupEntry
	lines := strings.Split(string(data), "\n")
	var currentChannel hdhr.LineupEntry
	forceHLS := runtime.HDHomeRunServer() != nil && runtime.HDHomeRunServer().PlexForceHLS()

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#EXTINF:") {
			if idx := strings.Index(line, `tvg-chno="`); idx != -1 {
				start := idx + 10
				if end := strings.Index(line[start:], `"`); end != -1 {
					currentChannel.GuideNumber = line[start : start+end]
				}
			}

			if idx := strings.LastIndex(line, ","); idx != -1 {
				currentChannel.GuideName = strings.TrimSpace(line[idx+1:])
			}
		} else if len(line) > 0 && !strings.HasPrefix(line, "#") && currentChannel.GuideName != "" {
			streamURL := line
			if forceHLS {
				streamURL = addHLSProxyPrefix(streamURL)
			}

			currentChannel.URL = streamURL
			lineup = append(lineup, currentChannel)
			currentChannel = hdhr.LineupEntry{}
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
