// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/problemcode"
	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
	"golang.org/x/sync/singleflight"
)

const (
	recordingThumbnailFilename        = "thumbnail.jpg"
	recordingThumbnailCacheControl    = "private, max-age=86400"
	recordingThumbnailDefaultSeekSec  = 45.0
	recordingThumbnailBuildTimeout    = 20 * time.Second
	recordingThumbnailProbeTimeout    = 8 * time.Second
	recordingThumbnailMaxWidth        = 1280
	recordingThumbnailMaxQuality      = "2"
	recordingThumbnailFallbackContent = "image/jpeg"
)

var recordingThumbnailBuildGroup singleflight.Group

func (s *Server) GetRecordingThumbnail(w http.ResponseWriter, r *http.Request, recordingId string) {
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
		log.L().Debug().Err(err).Str("recordingId", recordingId).Msg("recording thumbnail source unavailable")
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording thumbnail source not found", nil)
		return
	}

	thumbnailPath, err := recordingThumbnailPath(deps.cfg.HLS.Root, serviceRef)
	if err != nil {
		log.L().Warn().Err(err).Str("recordingId", recordingId).Msg("recording thumbnail cache unavailable")
		writeRegisteredProblem(w, r, http.StatusInternalServerError, "system/internal", "Internal Error", "INTERNAL_ERROR", "Thumbnail cache unavailable", nil)
		return
	}

	if err := ensureRecordingThumbnail(r.Context(), deps.cfg, sourcePath, thumbnailPath); err != nil {
		log.L().Debug().Err(err).Str("recordingId", recordingId).Str("sourcePath", sourcePath).Msg("recording thumbnail build failed")
		writeRegisteredProblem(w, r, http.StatusNotFound, "recordings/not-found", "Not Found", problemcode.CodeNotFound, "Recording thumbnail not found", nil)
		return
	}

	w.Header().Set("Cache-Control", recordingThumbnailCacheControl)
	w.Header().Set("Content-Type", recordingThumbnailFallbackContent)
	http.ServeFile(w, r, thumbnailPath)
}

func resolveRecordingThumbnailSourcePath(deps recordingsModuleDeps, serviceRef string) (string, error) {
	if deps.pathMapper == nil {
		return "", fmt.Errorf("recording path mapper not configured")
	}

	receiverPath := internalrecordings.ExtractPathFromServiceRef(serviceRef)
	if receiverPath == "" {
		return "", fmt.Errorf("recording receiver path missing")
	}

	localPath, ok := deps.pathMapper.ResolveLocalExisting(receiverPath)
	if !ok || strings.TrimSpace(localPath) == "" {
		return "", fmt.Errorf("recording path is not mapped locally")
	}

	info, err := os.Stat(localPath)
	if err != nil {
		return "", fmt.Errorf("stat recording source: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("recording source is a directory")
	}

	return localPath, nil
}

func recordingThumbnailPath(hlsRoot, serviceRef string) (string, error) {
	cacheDir, err := recservice.RecordingCacheDir(hlsRoot, serviceRef)
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, recordingThumbnailFilename), nil
}

func ensureRecordingThumbnail(ctx context.Context, cfg config.AppConfig, sourcePath, thumbnailPath string) error {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if thumbnailInfo, err := os.Stat(thumbnailPath); err == nil && recordingThumbnailIsFresh(sourceInfo, thumbnailInfo) {
		return nil
	}

	_, err, _ = recordingThumbnailBuildGroup.Do(thumbnailPath, func() (interface{}, error) {
		if thumbnailInfo, statErr := os.Stat(thumbnailPath); statErr == nil && recordingThumbnailIsFresh(sourceInfo, thumbnailInfo) {
			return nil, nil
		}

		if err := os.MkdirAll(filepath.Dir(thumbnailPath), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir thumbnail cache: %w", err)
		}

		seekSeconds := recordingThumbnailDefaultSeekSec
		if durationSeconds, probeErr := probeRecordingDurationSeconds(ctx, cfg, sourcePath); probeErr == nil {
			seekSeconds = resolveRecordingThumbnailSeekSeconds(durationSeconds)
		}

		tmpPath := strings.TrimSuffix(thumbnailPath, filepath.Ext(thumbnailPath)) + ".tmp" + filepath.Ext(thumbnailPath)
		if err := generateRecordingThumbnail(ctx, cfg, sourcePath, tmpPath, seekSeconds); err != nil {
			_ = os.Remove(tmpPath)
			return nil, err
		}

		info, err := os.Stat(tmpPath)
		if err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("stat thumbnail temp file: %w", err)
		}
		if info.IsDir() || info.Size() <= 0 {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("thumbnail temp file invalid")
		}

		if err := os.Rename(tmpPath, thumbnailPath); err != nil {
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("publish thumbnail: %w", err)
		}

		return nil, nil
	})
	return err
}

func recordingThumbnailIsFresh(sourceInfo, thumbnailInfo os.FileInfo) bool {
	if sourceInfo == nil || thumbnailInfo == nil {
		return false
	}
	if thumbnailInfo.IsDir() || thumbnailInfo.Size() <= 0 {
		return false
	}
	return !thumbnailInfo.ModTime().Before(sourceInfo.ModTime())
}

func probeRecordingDurationSeconds(ctx context.Context, cfg config.AppConfig, sourcePath string) (float64, error) {
	probeBin := strings.TrimSpace(cfg.FFmpeg.FFprobeBin)
	if probeBin == "" {
		probeBin = "ffprobe"
	}

	probeCtx, cancel := context.WithTimeout(ctx, recordingThumbnailProbeTimeout)
	defer cancel()

	cmd := exec.CommandContext( // #nosec G204 -- ffprobe binary is trusted config/default and args are fixed except resolver-confined sourcePath.
		probeCtx,
		probeBin,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		sourcePath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w", err)
	}

	durationSeconds, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	if durationSeconds <= 0 {
		return 0, fmt.Errorf("duration unavailable")
	}

	return durationSeconds, nil
}

func resolveRecordingThumbnailSeekSeconds(durationSeconds float64) float64 {
	if durationSeconds <= 0 {
		return recordingThumbnailDefaultSeekSec
	}

	seekSeconds := durationSeconds * 0.3
	if durationSeconds <= 30 {
		seekSeconds = math.Max(1, durationSeconds*0.2)
	} else if durationSeconds > 120 && seekSeconds < 12 {
		seekSeconds = 12
	}

	upperBound := durationSeconds - 2
	if upperBound > 0 && seekSeconds > upperBound {
		seekSeconds = upperBound
	}
	if seekSeconds < 0 {
		return 0
	}

	return seekSeconds
}

func generateRecordingThumbnail(ctx context.Context, cfg config.AppConfig, sourcePath, thumbnailPath string, seekSeconds float64) error {
	ffmpegBin := strings.TrimSpace(cfg.FFmpeg.Bin)
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}

	buildCtx, cancel := context.WithTimeout(ctx, recordingThumbnailBuildTimeout)
	defer cancel()

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
	}
	if seekSeconds > 0 {
		args = append(args, "-ss", formatThumbnailSeekSeconds(seekSeconds))
	}
	args = append(
		args,
		"-i", sourcePath,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", recordingThumbnailMaxWidth),
		"-q:v", recordingThumbnailMaxQuality,
		"-an",
		thumbnailPath,
	)

	cmd := exec.CommandContext(buildCtx, ffmpegBin, args...) // #nosec G204 -- ffmpeg binary is trusted config/default and args are internally constructed from resolver-confined paths.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg thumbnail: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func formatThumbnailSeekSeconds(seconds float64) string {
	if seconds <= 0 {
		return "0"
	}
	return strconv.FormatFloat(seconds, 'f', 3, 64)
}
