package api

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/jobs"
	"github.com/ManuGH/xg2g/internal/log"
)

// restoreCachedRefreshStatus makes an already generated channel playlist
// available to readiness immediately after restart. The full EPG refresh stays
// in the background and replaces this conservative snapshot when it finishes.
func (s *Server) restoreCachedRefreshStatus(cfg config.AppConfig) {
	playlistPath := filepath.Join(cfg.DataDir, s.snap.Runtime.PlaylistFilename)
	status, ok := cachedRefreshStatus(playlistPath, cfg.Version)
	if !ok {
		return
	}

	s.status = status
	logger := log.WithComponent("api")
	logger.Info().
		Int("channels", status.Channels).
		Time("playlist_updated_at", status.LastRun).
		Msg("restored cached playlist status for startup readiness")
}

func cachedRefreshStatus(playlistPath, version string) (jobs.Status, bool) {
	file, err := os.Open(filepath.Clean(playlistPath)) // #nosec G304 -- configured data-dir path
	if err != nil {
		return jobs.Status{}, false
	}
	defer func() { _ = file.Close() }()

	validHeader := false
	channels := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "#EXTM3U" {
			validHeader = true
		}
		if strings.HasPrefix(line, "#EXTINF:") {
			channels++
		}
	}
	if scanner.Err() != nil || !validHeader || channels == 0 {
		return jobs.Status{}, false
	}

	info, err := file.Stat()
	if err != nil {
		return jobs.Status{}, false
	}
	return jobs.Status{
		Version:  version,
		LastRun:  info.ModTime(),
		Channels: channels,
	}, true
}
