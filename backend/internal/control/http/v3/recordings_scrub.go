package v3

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ManuGH/xg2g/internal/config"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"golang.org/x/sync/singleflight"
)

// Scrub-preview frames power the timeline hover thumbnail for recordings
// (the VOD counterpart of the live-DVR preview.jpg). Offsets are aligned to
// a fixed grid so hovering across the timeline produces a bounded set of
// cacheable URLs, and each extracted frame is persisted next to the other
// per-recording artifacts, making repeat hovers and later sessions free.
const (
	recordingScrubIntervalSeconds = 10
	recordingScrubMaxWidth        = 320
	recordingScrubQuality         = "5"
	recordingScrubBuildTimeout    = recordingThumbnailBuildTimeout
	recordingScrubCacheControl    = "private, max-age=86400"
	recordingScrubDirName         = "scrub"
)

var recordingScrubBuildGroup singleflight.Group

// GetRecordingScrubFrame handles GET /recordings/{recordingId}/scrub.jpg?t=<seconds>.
func (s *Server) GetRecordingScrubFrame(w http.ResponseWriter, r *http.Request, recordingId string, params GetRecordingScrubFrameParams) {
	deps := s.recordingsModuleDeps()
	if _, ok := s.requireHouseholdRecordingAccess(w, r, recordingId); !ok {
		return
	}

	serviceRef, ok := recservice.DecodeRecordingID(recordingId)
	if !ok {
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording not found", nil)
		return
	}

	sourcePath, err := resolveRecordingThumbnailSourcePath(deps, serviceRef)
	if err != nil {
		log.L().Debug().Err(err).Str("recordingId", recordingId).Msg("recording scrub source unavailable")
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording scrub source not found", nil)
		return
	}

	offsetSeconds := alignScrubOffsetSeconds(params.T)

	framePath, err := recordingScrubFramePath(deps.cfg.HLS.Root, serviceRef, offsetSeconds)
	if err != nil {
		log.L().Warn().Err(err).Str("recordingId", recordingId).Msg("recording scrub cache unavailable")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", problemcode.CodeInternalError, "Scrub cache unavailable", nil)
		return
	}

	if err := ensureRecordingScrubFrame(r.Context(), deps.cfg, sourcePath, framePath, offsetSeconds); err != nil {
		log.L().Debug().Err(err).Str("recordingId", recordingId).Int64("offset", offsetSeconds).Msg("recording scrub frame build failed")
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording scrub frame not found", nil)
		return
	}

	w.Header().Set("Cache-Control", recordingScrubCacheControl)
	w.Header().Set("Content-Type", recordingThumbnailFallbackContent)
	http.ServeFile(w, r, framePath)
}

// alignScrubOffsetSeconds snaps the requested offset onto the preview grid so
// nearby hovers share one frame and cache entry.
func alignScrubOffsetSeconds(t *float32) int64 {
	if t == nil {
		return 0
	}
	v := float64(*t)
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
		return 0
	}
	return int64(v/recordingScrubIntervalSeconds) * recordingScrubIntervalSeconds
}

func recordingScrubFramePath(hlsRoot, serviceRef string, offsetSeconds int64) (string, error) {
	cacheDir, err := recservice.RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, recordingScrubDirName, fmt.Sprintf("t%d.jpg", offsetSeconds)), nil
}

func ensureRecordingScrubFrame(ctx context.Context, cfg config.AppConfig, sourcePath, framePath string, offsetSeconds int64) error {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if frameInfo, err := os.Stat(framePath); err == nil && recordingThumbnailIsFresh(sourceInfo, frameInfo) {
		return nil
	}

	_, err, _ = recordingScrubBuildGroup.Do(framePath, func() (any, error) {
		if frameInfo, statErr := os.Stat(framePath); statErr == nil && recordingThumbnailIsFresh(sourceInfo, frameInfo) {
			return nil, nil
		}

		if err := os.MkdirAll(filepath.Dir(framePath), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir scrub cache: %w", err)
		}

		tmpPath := strings.TrimSuffix(framePath, filepath.Ext(framePath)) + ".tmp" + filepath.Ext(framePath)
		if err := generateRecordingScrubFrame(ctx, cfg, sourcePath, tmpPath, offsetSeconds); err != nil {
			_ = os.Remove(tmpPath)
			return nil, err
		}

		info, err := os.Stat(tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("stat scrub temp file: %w", err)
		}
		if info.IsDir() || info.Size() <= 0 {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("scrub temp file invalid")
		}

		if err := os.Rename(tmpPath, framePath); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("publish scrub frame: %w", err)
		}

		return nil, nil
	})
	return err
}

func generateRecordingScrubFrame(ctx context.Context, cfg config.AppConfig, sourcePath, framePath string, offsetSeconds int64) error {
	ffmpegBin := strings.TrimSpace(cfg.FFmpeg.Bin)
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	buildCtx, cancel := context.WithTimeout(ctx, recordingScrubBuildTimeout)
	defer cancel()

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
	}
	if offsetSeconds > 0 {
		// Input seeking (-ss before -i) jumps via the demuxer index and only
		// decodes from the nearest preceding keyframe - fast even on long TS.
		args = append(args, "-ss", strconv.FormatInt(offsetSeconds, 10))
	}
	args = append(
		args,
		"-i", sourcePath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", recordingScrubMaxWidth),
		"-q:v", recordingScrubQuality,
		"-an",
		framePath,
	)

	cmd := exec.CommandContext(buildCtx, ffmpegBin, args...) // #nosec G204 -- ffmpeg binary is trusted config/default and args are internally constructed from resolver-confined paths.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg scrub frame: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}
