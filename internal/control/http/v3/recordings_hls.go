package v3

import (
	"io"
	"net/http"
	"os"
	"strconv"

	xg2ghttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/log"
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
	profile := detectClientProfile(r)

	var artifact artifacts.ArtifactOK
	var artErr *artifacts.ArtifactError

	if isTimeshift {
		artifact, artErr = s.artifacts.ResolveTimeshift(r.Context(), recordingId, string(profile))
	} else {
		artifact, artErr = s.artifacts.ResolvePlaylist(r.Context(), recordingId, string(profile))
	}

	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, artErr)
		return
	}

	// Apply SSOT Headers
	xg2ghttp.WriteHLSPlaylistHeaders(w, artifact.ModTime)

	// PR-P9-2: Range Policy A for Playlists (Explicitly 416 if range present)
	if r.Header.Get("Range") != "" {
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
}

// GetRecordingHLSCustomSegment handles GET /api/v3/recordings/{recordingId}/{segment}
func (s *Server) GetRecordingHLSCustomSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	s.serveHLSSegment(w, r, recordingId, segment, false)
}

func (s *Server) GetRecordingHLSCustomSegmentHead(w http.ResponseWriter, r *http.Request, recordingId string, segment string) {
	s.serveHLSSegment(w, r, recordingId, segment, true)
}

func (s *Server) serveHLSSegment(w http.ResponseWriter, r *http.Request, recordingId string, segment string, isHead bool) {
	artifact, artErr := s.artifacts.ResolveSegment(r.Context(), recordingId, segment)
	if artErr != nil {
		s.writeArtifactError(w, r, recordingId, artErr)
		return
	}

	f, err := os.Open(artifact.AbsPath)
	if err != nil {
		log.L().Error().Err(err).Str("path", artifact.AbsPath).Msg("failed to open ready segment")
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
		}
		return
	}

	// 3. Range Present (Policy A: Single-range)
	rng, err := xg2ghttp.ParseRange(rangeHeader, size)
	if err != nil {
		xg2ghttp.Write416(w, size)
		return
	}

	contentLength := rng.End - rng.Start + 1
	w.Header().Set("Content-Range", xg2ghttp.FormatContentRange(rng, size))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.WriteHeader(http.StatusPartialContent)

	if !isHead {
		if _, err := f.Seek(rng.Start, io.SeekStart); err != nil {
			return
		}
		_, _ = io.CopyN(w, f, contentLength)
	}
}

func (s *Server) writeArtifactError(w http.ResponseWriter, r *http.Request, recordingId string, err *artifacts.ArtifactError) {
	switch err.Code {
	case artifacts.CodePreparing:
		retrySec := 5
		if err.RetryAfter > 0 {
			retrySec = int(err.RetryAfter.Seconds())
			if retrySec < 1 {
				retrySec = 1
			}
		}
		s.writePreparingResponse(w, r, recordingId, "PREPARING", retrySec)
	case artifacts.CodeNotFound:
		RespondError(w, r, http.StatusNotFound, ErrRecordingNotFound, err.Detail)
	case artifacts.CodeInvalid:
		RespondError(w, r, http.StatusBadRequest, ErrInvalidInput, err.Detail)
	case artifacts.CodeInternal:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, err.Error())
	default:
		RespondError(w, r, http.StatusInternalServerError, ErrInternalServer, "internal error")
	}
}
