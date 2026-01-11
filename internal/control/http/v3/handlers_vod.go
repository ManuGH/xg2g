package v3

import (
	"context"
	"encoding/json"
	"errors"
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

	if s.vodResolver == nil {
		problem.Write(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "VOD Resolver not wired", nil)
		return
	}

	// Determine intent and profile (Deliverable #4)
	intent := types.IntentMetadata // Default for this endpoint
	clientProfile := s.mapProfile(r)
	profile := toPlaybackProfile(clientProfile)

	// 2. Resolve VOD (Facts)
	// Enforce SLO: Don't block UI thread for long probes.
	// We wait briefly (e.g. 150ms) to allow cache hits or fast DB lookups,
	// but return 503 if probe is stalling.
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Millisecond)
	defer cancel()

	mediaInfo, err := s.vodResolver.ResolveVOD(ctx, id, intent, profile)
	if err != nil {
		status := http.StatusNotFound
		code := "VOD_NOT_FOUND"
		msg := "Recording not found"

		if strings.Contains(err.Error(), "playback failed") {
			status = http.StatusInternalServerError
			code = "VOD_PLAYBACK_ERROR"
			msg = "Playback initialization failed"
		} else if strings.Contains(err.Error(), "metadata missing") {
			status = http.StatusUnprocessableEntity
			code = "VOD_METADATA_INVALID"
			msg = "Recording metadata incomplete"
		} else if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
			// SLO Hit: Probe is running but taking too long.
			status = http.StatusServiceUnavailable
			code = "PREPARING" // Match verification test expectation
			msg = "Media is being analyzed"
		}

		problem.Write(w, r, status, strings.ToLower(code), msg, code, err.Error(), nil)
		return
	}

	// Decision Engine
	decision, err := playback.Decide(profile, mediaInfo, playback.Policy{})
	if err != nil {
		// Should act on ModeError
		problem.Write(w, r, http.StatusInternalServerError, "vod/decision-error", "Playback Decision Failed", "DECISION_ERROR", err.Error(), nil)
		return
	}
	if decision.Mode == playback.ModeError {
		problem.Write(w, r, http.StatusNotFound, "vod/probe-failed", "Media Probe Failed", "PROBE_FAILED", string(decision.Reason), nil)
		return
	}

	// Map Decision to DTO
	resp := types.VODPlaybackResponse{
		RecordingID:     id,
		StreamURL:       s.mapDecisionToURL(id, decision, mediaInfo),
		PlaybackType:    string(decision.Mode),
		DurationSeconds: int64(mediaInfo.Duration),
		// Legacy fields if needed
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func toPlaybackProfile(p types.ClientProfile) playback.ClientProfile {
	// Simple mapping based on Name (assuming mapProfile does UA parsing)
	// Or we should verify if Name is reliable.
	return playback.ClientProfile{
		UserAgent: p.Name,
		IsSafari:  strings.Contains(p.Name, "Safari") && !strings.Contains(p.Name, "Chrome"),
		IsChrome:  strings.Contains(p.Name, "Chrome"),
		// Capabilities could be mapped from p.Containers/Codecs
		CanPlayTS: contains(p.Containers, "mpegts"),
		// ...
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

// mapDecisionToURL constructs the canonical URL based on decision.
// P4: Canonical URL Mapping
func (s *Server) mapDecisionToURL(id string, d playback.Decision, m playback.MediaInfo) string {
	// Determine extension based on Artifact
	ext := "mp4"
	if d.Artifact == playback.ArtifactHLS {
		ext = "m3u8"
	}
	// If DirectPlay and source is TS, we might want stream.ts?
	// But handler usually listens on fixed paths.
	// If we use /stream.mp4 or /playlist.m3u8

	// Use canonical builder
	return fmt.Sprintf("/api/v3/vod/%s/stream.%s", id, ext)
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
		p.SupportsHLS = true // Modern Chrome supports HLS via hls.js usually, but here we mean native or preferred
	}

	return p
}
