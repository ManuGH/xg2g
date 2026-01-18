// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/ManuGH/xg2g/internal/control/auth"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/hls"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
)

// Responsibility: Handles truthful playback capability probing.
// Non-goals: Actual serving of media (see handlers_hls.go).

// GetRecordingPlaybackInfo implements ServerInterface
func (s *Server) GetRecordingPlaybackInfo(w http.ResponseWriter, r *http.Request, recordingId string) {
	// 1. Safety: Service Access
	s.mu.RLock()
	svc := s.recordingsService
	s.mu.RUnlock()

	if svc == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "system/unavailable", "Service Unavailable", "UNAVAILABLE", "Recordings service is not initialized", nil)
		return
	}

	// Determine Client Profile
	profile := detectClientProfile(r)

	// 2. Delegate to Service (Strict Resolution)
	// Handlers are thin adapters: pass raw Hex ID, Service owns decoding.
	resolution, err := s.recordingsService.ResolvePlayback(r.Context(), recordingId, string(profile))

	// 3. Map Errors to HTTP Status (Fail-closed Policy)
	if err != nil {
		s.mapPlaybackError(w, r, recordingId, err)
		return
	}

	// 3b. Segment Truth Extraction (PR-P3-4)
	var segmentTruth *hls.SegmentTruth
	var attemptedTruth bool
	if s.artifacts != nil && resolution.Strategy == recordings.StrategyHLS {
		attemptedTruth = true
		// We must inspect the playlist to determine truth
		if artifact, artErr := s.artifacts.ResolvePlaylist(r.Context(), recordingId, string(profile)); artErr == nil {
			content, readErr := readArtifactContent(artifact)
			if readErr == nil {
				truth, extractErr := hls.ExtractSegmentTruth(content)
				if extractErr == nil {
					segmentTruth = truth
				} else {
					log.L().Warn().Err(extractErr).Str("id", recordingId).Msg("hls truth extraction failed")
					// segmentTruth remains nil -> mapPlaybackInfo will handle fail-closed
				}
			} else {
				log.L().Warn().Err(readErr).Str("id", recordingId).Msg("failed to read hls playlist for truth")
			}
		}
	}

	// 4. Resolve Resume State (User Context)
	var resumeState *resume.State
	if s.resumeStore != nil {
		if p := auth.PrincipalFromContext(r.Context()); p != nil {
			// Best-effort resume fetch. If store fails, we just don't return resume.
			// Currently using the raw recordingId (encoded) as the key, consistent with headers.
			if stored, err := s.resumeStore.Get(r.Context(), p.ID, recordingId); err == nil {
				resumeState = stored
			}
		}
	}

	// 5. Transform to DTO (Fail-closed Mapping)
	// We map ONLY what is strictly known.
	dto := s.mapPlaybackInfo(r.Context(), recordingId, resolution, resumeState, segmentTruth, attemptedTruth)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto)
}

func (s *Server) mapPlaybackError(w http.ResponseWriter, r *http.Request, id string, err error) {
	// Use existing classification logic from recordings.go
	// Since we don't have direct access to 'writeRecordingError' cleanly from this file without duplication,
	// we re-implement the classification switch here for strictness.
	class := recordings.Classify(err)
	msg := err.Error()

	switch class {
	case recordings.ClassInvalidArgument:
		writeProblem(w, r, http.StatusBadRequest, "recordings/invalid", "Invalid Request", "INVALID_INPUT", msg, nil)
	case recordings.ClassNotFound:
		writeProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", "NOT_FOUND", msg, nil)
	case recordings.ClassForbidden:
		writeProblem(w, r, http.StatusForbidden, "recordings/forbidden", "Access Denied", "FORBIDDEN", msg, nil)
	case recordings.ClassPreparing:
		w.Header().Set("Retry-After", "5")
		writeProblem(w, r, http.StatusServiceUnavailable, "recordings/preparing", "Preparing", "PREPARING", msg, nil)
	case recordings.ClassUnsupported:
		writeProblem(w, r, http.StatusUnprocessableEntity, "recordings/remote-probe-unsupported", "Remote Probe Unsupported", "REMOTE_PROBE_UNSUPPORTED", msg, nil)
	case recordings.ClassUpstream:
		writeProblem(w, r, http.StatusBadGateway, "recordings/upstream", "Upstream Error", "UPSTREAM_ERROR", msg, nil)
	default:
		log.L().Error().Err(err).Str("id", id).Msg("playback resolution failed")
		writeProblem(w, r, http.StatusInternalServerError, "playback/resolution_failed", "Resolution Failed", "INTERNAL_ERROR", "Failed to resolve playback info", nil)
	}
}

// mapPlaybackInfo maps the internal resolution to the truthful PlaybackInfo DTO.
// Strict fail-closed policy. id must be the raw Hex ID for URL construction.
func (s *Server) mapPlaybackInfo(ctx context.Context, id string, d recordings.PlaybackResolution, rState *resume.State, truth *hls.SegmentTruth, attemptedTruth bool) PlaybackInfo {
	// Strict Mapping: No Defaults.

	mode := DirectMp4
	// URL Construction: Only Handler knows routes.
	// TODO: Base URL injection if absolute URL needed. Relative for now.
	url := fmt.Sprintf("/api/v3/recordings/%s/stream.mp4", id)

	if d.Strategy == recordings.StrategyHLS {
		mode = Hls
		url = fmt.Sprintf("/api/v3/recordings/%s/playlist.m3u8", id)
	}

	// Deterministic fields
	// P3-4: Seekable logic depends on Truth if HLS
	canSeek := d.CanSeek // Default from resolver (usually true for VODs)

	var (
		dvrWindow  *int64
		startUnix  *int64
		liveEdge   *int64
		isSeekable *bool
	)

	if d.Strategy == recordings.StrategyHLS {
		if truth != nil {
			if truth.IsVOD {
				// Case C: VOD (Robust)
				// Seekable = true, Window = Duration
				cs := true
				isSeekable = &cs
				canSeek = true

				dur := int64(truth.TotalDuration.Seconds())
				dvrWindow = &dur
			} else {
				// Case A/B: Live/Event
				if truth.HasPDT {
					s := truth.FirstPDT.Unix()
					// LiveEdge = LastPDT + LastDuration
					edge := truth.LastPDT.Add(truth.LastDuration).Unix()
					w := edge - s

					// Guard: Plausibility (Stop-the-line)
					if w <= 0 || edge <= s {
						// Case D: Implausible Live (Broken Timestamps)
						// Fail-Closed
						cs := false
						isSeekable = &cs
						canSeek = false
					} else {
						// Case A: Valid Live
						cs := true
						isSeekable = &cs
						canSeek = true

						startUnix = &s
						liveEdge = &edge
						dvrWindow = &w
					}
				} else {
					// Case B: Broken Live (Missing PDT)
					// Fail-Closed
					cs := false
					isSeekable = &cs
					canSeek = false
				}
			}
		} else {
			// Extraction Failed or Not Attempted
			if attemptedTruth {
				// We tried to extract truth and failed (Corrupt Playlist, IO Error)
				// Stop-the-line: Fail Closed.
				cs := false
				isSeekable = &cs
				canSeek = false
			} else {
				// We didn't try (Testing environment without Artifacts)
				// Fallback to legacy
			}
		}
	}

	if isSeekable == nil {
		isSeekable = &canSeek
	}

	// Duration Source Truth (Strict Enums)
	var durSrc *PlaybackInfoDurationSource
	if d.DurationSource != nil {
		switch *d.DurationSource {
		case recordings.DurationSourceStore:
			s := Store
			durSrc = &s
		case recordings.DurationSourceCache:
			s := Cache
			durSrc = &s
		case recordings.DurationSourceProbe:
			s := Probe
			durSrc = &s
		}
	}

	// Reason Enum Mapping (Strict)
	var reason PlaybackInfoReason
	switch d.Reason {
	case recordings.ReasonDirectPlayMatch:
		reason = PlaybackInfoReasonDirectplayMatch
	case recordings.ReasonTranscodeAudio:
		reason = PlaybackInfoReasonTranscodeAudio
	case recordings.ReasonTranscodeVideo:
		reason = PlaybackInfoReasonTranscodeVideo
	case "transcode_all": // Future proofing against string literals not yet in constants
		reason = PlaybackInfoReasonTranscodeAll
	case "container_mismatch":
		reason = PlaybackInfoReasonContainerMismatch
	default:
		reason = PlaybackInfoReasonUnknown
	}

	var resDTO *struct {
		DurationSeconds *int64  `json:"duration_seconds,omitempty"`
		Finished        *bool   `json:"finished,omitempty"`
		PosSeconds      float32 `json:"pos_seconds"`
	}

	if rState != nil {
		fin := rState.Finished
		var dur *int64
		if rState.DurationSeconds > 0 {
			v := rState.DurationSeconds
			dur = &v
		}
		resDTO = &struct {
			DurationSeconds *int64  `json:"duration_seconds,omitempty"`
			Finished        *bool   `json:"finished,omitempty"`
			PosSeconds      float32 `json:"pos_seconds"`
		}{
			PosSeconds:      float32(rState.PosSeconds),
			DurationSeconds: dur,
			Finished:        &fin,
		}
	}

	return PlaybackInfo{
		Mode:            mode,
		Url:             url,
		Seekable:        isSeekable,
		IsSeekable:      isSeekable,    // P3-4 New Field
		DurationSeconds: d.DurationSec, // Pass-through pointer
		DurationSource:  durSrc,
		Container:       d.Container,  // Pass-through pointer
		VideoCodec:      d.VideoCodec, // Pass-through pointer
		AudioCodec:      d.AudioCodec, // Pass-through pointer
		Reason:          &reason,
		Resume:          resDTO,
		RequestId:       log.RequestIDFromContext(ctx), // Source of Truth
		SessionId:       fmt.Sprintf("rec:%s", id),     // Namespaced Session ID
		// P3-4 Truth Fields
		StartUnix:        startUnix,
		LiveEdgeUnix:     liveEdge,
		DvrWindowSeconds: dvrWindow,
	}
}

func readArtifactContent(a artifacts.ArtifactOK) (string, error) {
	if a.Data != nil {
		return string(a.Data), nil
	}
	if a.AbsPath != "" {
		b, err := os.ReadFile(a.AbsPath)
		return string(b), err
	}
	return "", fmt.Errorf("empty artifact")
}
