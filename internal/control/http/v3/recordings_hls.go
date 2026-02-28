package v3

import (
	"io"
	"net/http"
	"os"
	"strconv"

	xg2ghttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/metrics"
)

// PR-P9-2: HLS Handlers (Hardened Contract)

// GetRecordingHLSPlaylist handles GET /api/v3/recordings/{recordingId}/playlist.m3u8
func (s *Server) GetRecordingHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.serveHLSPlaylist(w, r, recordingId, false, false)
}

func (s *Server) GetRecordingHLSPlaylistHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.serveHLSPlaylist(w, r, recordingId, false, true)
}

// GetRecordingHLSTimeshift handles GET /api/v3/recordings/{recordingId}/timeshift.m3u8
func (s *Server) GetRecordingHLSTimeshift(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.serveHLSPlaylist(w, r, recordingId, true, false)
}

func (s *Server) GetRecordingHLSTimeshiftHead(w http.ResponseWriter, r *http.Request, recordingId string) {
	s.serveHLSPlaylist(w, r, recordingId, true, true)
}

func (s *Server) serveHLSPlaylist(w http.ResponseWriter, r *http.Request, recordingId string, isTimeshift bool, isHead bool) {
	deps := s.recordingsModuleDeps()
	profile := detectClientProfile(r)

	var artifact artifacts.ArtifactOK
	var artErr *artifacts.ArtifactError

	if isTimeshift {
		artifact, artErr = s.artifacts.ResolveTimeshift(r.Context(), recordingId, string(profile))
	} else {
		artifact, artErr = s.artifacts.ResolvePlaylist(r.Context(), recordingId, string(profile))
	}

	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, playbackStagePlaylistLabel, artErr)
		return
	}

	// Apply SSOT Headers
	xg2ghttp.WriteHLSPlaylistHeaders(w, artifact.ModTime)

	// PR-P9-2: Range Policy A for Playlists (Explicitly 416 if range present)
	if r.Header.Get("Range") != "" {
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, playbackStagePlaylistLabel, "INVALID_INPUT")
		size := int64(len(artifact.Data))
		if artifact.AbsPath != "" {
			if info, err := os.Stat(artifact.AbsPath); err == nil {
				size = info.Size()
			}
		}
		xg2ghttp.Write416(w, size)
		return
	}

	// Timeshift specific override if needed (though ResolveTimeshift might already set it,
	// we enforce it here for contract truth)
	if isTimeshift {
		w.Header().Set("Cache-Control", "no-store")
	}

	w.Header().Set("X-Playback-Session-Id", "rec:"+recordingId)
	w.Header().Set("Content-Length", strconv.Itoa(len(artifact.Data)))
	w.WriteHeader(http.StatusOK)
	if !isHead {
		if artifact.Data != nil {
			_, _ = w.Write(artifact.Data)
		} else if artifact.AbsPath != "" {
			f, err := os.Open(artifact.AbsPath)
			if err == nil {
				defer func() {
					if err := f.Close(); err != nil {
						// best-effort close
						log.L().Debug().Err(err).Msg("failed to close playlist file")
					}
				}()
				_, _ = io.Copy(w, f)
			}
		}
	}
	if !isHead && deps.playbackSLO != nil {
		obs := deps.playbackSLO.MarkMediaSuccess(playbackSessionMeta{
			SessionID:   "rec:" + recordingId,
			Schema:      playbackSchemaRecordingLabel,
			Mode:        playbackModeHLSLabel,
			RecordingID: recordingId,
		})
		if obs.TTFFObserved {
			log.L().Info().
				Str("event", "playback.slo.ttff").
				Str("request_id", requestID(r.Context())).
				Str("session_id", "rec:"+recordingId).
				Str("schema", obs.Schema).
				Str("mode", obs.Mode).
				Str("outcome", "ok").
				Str("recording_id", recordingId).
				Float64("ttff_seconds", obs.TTFFSeconds).
				Msg("recording playback ttff observed")
		}
		if obs.RebufferSeverity != "" {
			log.L().Warn().
				Str("event", "playback.slo.rebuffer").
				Str("request_id", requestID(r.Context())).
				Str("session_id", "rec:"+recordingId).
				Str("schema", obs.Schema).
				Str("mode", obs.Mode).
				Str("severity", obs.RebufferSeverity).
				Str("recording_id", recordingId).
				Msg("recording playback rebuffer proxy event observed")
		}
	}
}

// GetRecordingHLSCustomSegment handles GET /api/v3/recordings/{recordingId}/{segment}
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	s.serveHLSSegment(w, r, recordingId, segment, false)
}

func (s *Server) GetRecordingHLSCustomSegmentHead(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	s.serveHLSSegment(w, r, recordingId, segment, true)
}

func (s *Server) serveHLSSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string, isHead bool) {
	deps := s.recordingsModuleDeps()
	artifact, artErr := s.artifacts.ResolveSegment(r.Context(), recordingId, segment)
	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, playbackStageSegmentLabel, artErr)
		return
	}

	f, err := os.Open(artifact.AbsPath)
	if err != nil {
		log.L().Error().Err(err).Str("path", artifact.AbsPath).Msg("failed to open ready segment")
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, playbackStageSegmentLabel, "INTERNAL_ERROR")
		if deps.playbackSLO != nil {
			outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
				SessionID:   "rec:" + recordingId,
				Schema:      playbackSchemaRecordingLabel,
				Mode:        playbackModeHLSLabel,
				RecordingID: recordingId,
			}, "failed")
			if outcome.TTFFObserved {
				log.L().Info().
					Str("event", "playback.slo.ttff").
					Str("request_id", requestID(r.Context())).
					Str("session_id", "rec:"+recordingId).
					Str("schema", outcome.Schema).
					Str("mode", outcome.Mode).
					Str("outcome", outcome.Outcome).
					Str("recording_id", recordingId).
					Float64("ttff_seconds", outcome.TTFFSeconds).
					Msg("recording playback ttff outcome observed")
			}
		}
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "failed to open segment")
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.L().Debug().Err(err).Str("path", artifact.AbsPath).Msg("failed to close segment file")
		}
	}()

	info, err := f.Stat()
	if err != nil {
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, playbackStageSegmentLabel, "INTERNAL_ERROR")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "stat failed")
		return
	}
	size := info.Size()

	// 1. Apply Base Headers
	isInit := artifact.Kind == artifacts.ArtifactKindSegmentInit
	isFMP4 := artifact.Kind == artifacts.ArtifactKindSegmentFMP4
	xg2ghttp.WriteHLSSegmentHeaders(w, artifact.ModTime, isInit, isFMP4)

	// 2. Handle Range
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(http.StatusOK)
		if !isHead {
			_, _ = io.Copy(w, f)
			if deps.playbackSLO != nil {
				obs := deps.playbackSLO.MarkMediaSuccess(playbackSessionMeta{
					SessionID:   "rec:" + recordingId,
					Schema:      playbackSchemaRecordingLabel,
					Mode:        playbackModeHLSLabel,
					RecordingID: recordingId,
				})
				if obs.TTFFObserved {
					log.L().Info().
						Str("event", "playback.slo.ttff").
						Str("request_id", requestID(r.Context())).
						Str("session_id", "rec:"+recordingId).
						Str("schema", obs.Schema).
						Str("mode", obs.Mode).
						Str("outcome", "ok").
						Str("recording_id", recordingId).
						Float64("ttff_seconds", obs.TTFFSeconds).
						Msg("recording playback ttff observed")
				}
				if obs.RebufferSeverity != "" {
					log.L().Warn().
						Str("event", "playback.slo.rebuffer").
						Str("request_id", requestID(r.Context())).
						Str("session_id", "rec:"+recordingId).
						Str("schema", obs.Schema).
						Str("mode", obs.Mode).
						Str("severity", obs.RebufferSeverity).
						Str("recording_id", recordingId).
						Msg("recording playback rebuffer proxy event observed")
				}
			}
		}
		return
	}

	// 3. Range Present (Policy A: Single-range)
	rng, err := xg2ghttp.ParseRange(rangeHeader, size)
	if err != nil {
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, playbackStageSegmentLabel, "INVALID_INPUT")
		xg2ghttp.Write416(w, size)
		return
	}

	contentLength := rng.End - rng.Start + 1
	w.Header().Set("Content-Range", xg2ghttp.FormatContentRange(rng, size))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.WriteHeader(http.StatusPartialContent)

	if !isHead {
		if _, err := f.Seek(rng.Start, io.SeekStart); err != nil {
			metrics.IncPlaybackError(playbackSchemaRecordingLabel, playbackStageSegmentLabel, "INTERNAL_ERROR")
			return
		}
		_, _ = io.CopyN(w, f, contentLength)
		if deps.playbackSLO != nil {
			obs := deps.playbackSLO.MarkMediaSuccess(playbackSessionMeta{
				SessionID:   "rec:" + recordingId,
				Schema:      playbackSchemaRecordingLabel,
				Mode:        playbackModeHLSLabel,
				RecordingID: recordingId,
			})
			if obs.TTFFObserved {
				log.L().Info().
					Str("event", "playback.slo.ttff").
					Str("request_id", requestID(r.Context())).
					Str("session_id", "rec:"+recordingId).
					Str("schema", obs.Schema).
					Str("mode", obs.Mode).
					Str("outcome", "ok").
					Str("recording_id", recordingId).
					Float64("ttff_seconds", obs.TTFFSeconds).
					Msg("recording playback ttff observed")
			}
			if obs.RebufferSeverity != "" {
				log.L().Warn().
					Str("event", "playback.slo.rebuffer").
					Str("request_id", requestID(r.Context())).
					Str("session_id", "rec:"+recordingId).
					Str("schema", obs.Schema).
					Str("mode", obs.Mode).
					Str("severity", obs.RebufferSeverity).
					Str("recording_id", recordingId).
					Msg("recording playback rebuffer proxy event observed")
			}
		}
	}
}

func (s *Server) writeArtifactError(w http.ResponseWriter, r *http.Request, recordingId string, stage string, err *artifacts.ArtifactError) {
	deps := s.recordingsModuleDeps()
	switch err.Code {
	case artifacts.CodePreparing:
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, stage, "RECORDING_PREPARING")
		retrySec := 5
		if err.RetryAfter > 0 {
			retrySec = int(err.RetryAfter.Seconds())
			if retrySec < 1 {
				retrySec = 1
			}
		}
		s.writePreparingResponse(w, r, recordingId, "PREPARING", retrySec)
	case artifacts.CodeNotFound:
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, stage, "NOT_FOUND")
		if deps.playbackSLO != nil {
			outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
				SessionID:   "rec:" + recordingId,
				Schema:      playbackSchemaRecordingLabel,
				Mode:        playbackModeHLSLabel,
				RecordingID: recordingId,
			}, "failed")
			if outcome.TTFFObserved {
				log.L().Info().
					Str("event", "playback.slo.ttff").
					Str("request_id", requestID(r.Context())).
					Str("session_id", "rec:"+recordingId).
					Str("schema", outcome.Schema).
					Str("mode", outcome.Mode).
					Str("outcome", outcome.Outcome).
					Str("recording_id", recordingId).
					Float64("ttff_seconds", outcome.TTFFSeconds).
					Msg("recording playback ttff outcome observed")
			}
		}
		RespondError(w, r, http.StatusNotFound, ErrRecordingNotFound, err.Detail)
	case artifacts.CodeInvalid:
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, stage, "INVALID_INPUT")
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Detail)
	case artifacts.CodeInternal:
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, stage, "INTERNAL_ERROR")
		if deps.playbackSLO != nil {
			outcome := deps.playbackSLO.MarkOutcome(playbackSessionMeta{
				SessionID:   "rec:" + recordingId,
				Schema:      playbackSchemaRecordingLabel,
				Mode:        playbackModeHLSLabel,
				RecordingID: recordingId,
			}, "failed")
			if outcome.TTFFObserved {
				log.L().Info().
					Str("event", "playback.slo.ttff").
					Str("request_id", requestID(r.Context())).
					Str("session_id", "rec:"+recordingId).
					Str("schema", outcome.Schema).
					Str("mode", outcome.Mode).
					Str("outcome", outcome.Outcome).
					Str("recording_id", recordingId).
					Float64("ttff_seconds", outcome.TTFFSeconds).
					Msg("recording playback ttff outcome observed")
			}
		}
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
	default:
		metrics.IncPlaybackError(playbackSchemaRecordingLabel, stage, "INTERNAL_ERROR")
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "internal error")
	}
}
