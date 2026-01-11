package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/control/http/v3/problem"
	"github.com/ManuGH/xg2g/internal/control/http/v3/types"
	"github.com/ManuGH/xg2g/internal/control/playback"
)

// VODResolver defines the boundary between HTTP and VOD Engine.
// It resolves a recording ID to playback metadata (Pure Facts).
type VODResolver interface {
	ResolveVOD(ctx context.Context, recordingID string, intent types.PlaybackIntent, profile playback.ClientProfile) (playback.MediaInfo, error)
}

// HandleVODPlaybackInfo resolves playback assets for a recording.
// GET /api/v3/vod/{recordingId}
// Scopes: v3:read
func (s *Server) HandleVODPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "recordingId")
	if id == "" {
		problem.Write(w, r, http.StatusNotFound, "vod/not-found", "Not Found", "NOT_FOUND", "Recording ID required", nil)
		return
	}

	if s.resolver == nil {
		problem.Write(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Resolver not wired", nil)
		return
	}

	// Determine intent and profile (Deliverable #4)
	intent := types.IntentMetadata // Default for this endpoint
	clientProfile := s.mapProfile(r)
	profile := toPlaybackProfile(clientProfile)

	// 2. Resolve VOD (Facts)
	// Enforce SLO for UI responsiveness
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Millisecond)
	defer cancel()

	res, prob := s.resolver.Resolve(ctx, id, intent, profile)

	if prob != nil {
		s.writeResolveError(w, r, prob)
		return
	}

	// Map Decision to URL
	streamURL, err := s.mapDecisionToURL(id, res.Decision, res.MediaInfo)
	if err != nil {
		// Should not happen if resolver succeeded, but handle it
		problem.Write(w, r, http.StatusInternalServerError, "vod/url-generation-failed", "URL Generation Failed", "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	mode, err := mapDecisionToPlaybackMode(res.Decision)
	if err != nil {
		problem.Write(w, r, http.StatusInternalServerError, "vod/invalid_mode", "VOD Error", "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	// Map Decision to DTO
	resp := types.VODPlaybackResponse{
		Mode:            mode,
		URL:             streamURL,
		DurationSeconds: int64(res.MediaInfo.Duration),
		Reason:          res.Reason,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func toPlaybackProfile(p types.ClientProfile) playback.ClientProfile {
	// Simple mapping based on Name (assuming mapProfile does UA parsing)
	return playback.ClientProfile{
		UserAgent: p.Name,
		IsSafari:  strings.Contains(p.Name, "Safari") && !strings.Contains(p.Name, "Chrome"),
		IsChrome:  strings.Contains(p.Name, "Chrome"),
		CanPlayTS: contains(p.Containers, "mpegts"),
	}
}

func contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// mapDecisionToURL converts a playback decision into a client-facing URL.
func (s *Server) mapDecisionToURL(serviceRef string, decision playback.Decision, meta playback.MediaInfo) (string, error) {
	switch decision.Mode {
	case playback.ModeDirectPlay, playback.ModeDirectStream:
		// Direct file or remux
		return fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", serviceRef), nil
	case playback.ModeTranscode:
		// HLS Playlist
		return fmt.Sprintf("/api/v3/recordings/%s/index.m3u8", serviceRef), nil
	case playback.ModeError:
		return "", fmt.Errorf("decision indicates playability error")
	}
	// Fallback/Unknown
	return "", fmt.Errorf("unknown mode: %s", decision.Mode)
}

func mapDecisionToPlaybackMode(decision playback.Decision) (string, error) {
	switch decision.Mode {
	case playback.ModeDirectPlay, playback.ModeDirectStream:
		return string(DirectMp4), nil
	case playback.ModeTranscode:
		return string(Hls), nil
	case playback.ModeError:
		return "", fmt.Errorf("decision indicates playability error")
	}
	return "", fmt.Errorf("unknown mode: %s", decision.Mode)
}

func (s *Server) mapProfile(r *http.Request) types.ClientProfile {
	ua := r.Header.Get("User-Agent")
	p := types.ClientProfile{
		Name:        "Unknown",
		VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac", "mp3"},
		Containers:  []string{"mp4", "ts"},
		SupportsHLS: false,
	}

	if strings.Contains(ua, "Safari") && !strings.Contains(ua, "Chrome") {
		p.Name = "Safari"
		p.VideoCodecs = append(p.VideoCodecs, "hevc")
		p.SupportsHLS = true
	} else if strings.Contains(ua, "VLC") {
		p.Name = "VLC"
		p.VideoCodecs = append(p.VideoCodecs, "hevc", "mpeg2")
		p.SupportsHLS = true
	} else if strings.Contains(ua, "Chrome") {
		p.Name = "Chrome"
		p.SupportsHLS = true
	}

	return p
}
