package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type fpsCacheEntry struct {
	FPS       int
	LearnedAt time.Time
}

func (a *LocalAdapter) learnFPSFromOutput(sourceKey, sessionID string) {
	if sourceKey == "" || sessionID == "" {
		return
	}
	sessionDir := filepath.Join(a.HLSRoot, "sessions", sessionID)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		segment, ok := findFirstOutputSegment(sessionDir)
		if ok {
			fps, basis, err := a.probeOutputFPS(segment)
			if err != nil {
				a.Logger.Debug().
					Str("sessionId", sessionID).
					Str("source_key", sourceKey).
					Str("segment", segment).
					Err(err).
					Msg("failed to learn fps from output segment")
				return
			}
			if fps < a.FPSMin || fps > a.FPSMax {
				a.Logger.Debug().
					Str("sessionId", sessionID).
					Str("source_key", sourceKey).
					Str("segment", segment).
					Int("fps", fps).
					Int("fps_min", a.FPSMin).
					Int("fps_max", a.FPSMax).
					Msg("ignored learned fps from output segment (out of range)")
				return
			}
			a.setLastKnownFPS(sourceKey, fps)
			a.Logger.Info().
				Str("sessionId", sessionID).
				Str("source_key", sourceKey).
				Str("segment", segment).
				Int("fps", fps).
				Str("fps_basis", "encoder_output_"+basis).
				Msg("learned fps from encoder output")
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func findFirstOutputSegment(sessionDir string) (string, bool) {
	patterns := []string{"seg_*.m4s", "seg_*.ts", "*.m4s", "*.ts"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(sessionDir, pattern))
		if err != nil || len(matches) == 0 {
			continue
		}
		sort.Strings(matches)
		for _, filePath := range matches {
			info, err := os.Stat(filePath)
			if err != nil || info.Size() <= 0 {
				continue
			}
			return filePath, true
		}
	}
	return "", false
}

func (a *LocalAdapter) probeOutputFPS(segmentPath string) (int, string, error) {
	probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ffprobeBin := a.FFprobeBin
	if strings.TrimSpace(ffprobeBin) == "" {
		ffprobeBin = "ffprobe"
	}

	args := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate,avg_frame_rate",
		"-of", "default=noprint_wrappers=1",
		segmentPath,
	}
	// #nosec G204 -- ffprobe bin path is trusted
	cmd := exec.CommandContext(probeCtx, ffprobeBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return 0, "", decorateProbeError(err, stderr.String())
	}
	output := strings.TrimSpace(string(out))
	if output == "" {
		return 0, "", fmt.Errorf("empty output")
	}
	return parseFPSProbeOutput(output)
}

func shouldRetryFPSProbe(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "signal: killed") || strings.Contains(msg, "deadline exceeded")
}

func decorateProbeError(err error, stderr string) error {
	if err == nil {
		return nil
	}
	clean := trimForLog(stderr, 512)
	if clean == "" {
		return err
	}
	return fmt.Errorf("%w (stderr: %s)", err, clean)
}

func trimForLog(raw string, max int) string {
	flat := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if flat == "" || max <= 0 || len(flat) <= max {
		return flat
	}
	return flat[:max] + "..."
}

func (a *LocalAdapter) setLastKnownFPS(sourceKey string, fps int) {
	if sourceKey == "" || fps <= 0 {
		return
	}
	a.fpsCacheMu.Lock()
	a.lastKnownFPS[sourceKey] = fpsCacheEntry{
		FPS:       fps,
		LearnedAt: time.Now(),
	}
	a.fpsCacheMu.Unlock()
}

func (a *LocalAdapter) getLastKnownFPS(sourceKey string) (int, bool) {
	if sourceKey == "" {
		return 0, false
	}
	ttl := a.FPSCacheTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now()

	a.fpsCacheMu.Lock()
	defer a.fpsCacheMu.Unlock()
	entry, ok := a.lastKnownFPS[sourceKey]
	if !ok {
		return 0, false
	}
	if entry.LearnedAt.IsZero() || now.Sub(entry.LearnedAt) > ttl {
		delete(a.lastKnownFPS, sourceKey)
		return 0, false
	}
	return entry.FPS, true
}

func (a *LocalAdapter) probeFPS(ctx context.Context, inputURL string) (int, string, error) {
	if a.fpsProbeFn != nil {
		return a.fpsProbeFn(ctx, inputURL)
	}
	return a.detectFPS(ctx, inputURL)
}

func (a *LocalAdapter) buildFPSProbeArgs(inputURL string, retry bool) []string {
	analyzeDuration := strings.TrimSpace(a.FPSProbeAnalyze)
	if analyzeDuration == "" {
		analyzeDuration = strings.TrimSpace(a.AnalyzeDuration)
	}
	probeSize := strings.TrimSpace(a.FPSProbeSize)
	if probeSize == "" {
		probeSize = strings.TrimSpace(a.ProbeSize)
	}
	if retry {
		if v := strings.TrimSpace(a.FPSProbeRetryAn); v != "" {
			analyzeDuration = v
		}
		if v := strings.TrimSpace(a.FPSProbeRetrySize); v != "" {
			probeSize = v
		}
	}

	args := []string{
		"-v", "error",
	}
	if v := strings.TrimSpace(a.FPSProbeFFlags); v != "" {
		args = append(args, "-fflags", v)
	}
	if v := strings.TrimSpace(a.FPSProbeErrDetect); v != "" {
		args = append(args, "-err_detect", v)
	}
	if analyzeDuration != "" {
		args = append(args, "-analyzeduration", analyzeDuration)
	}
	if probeSize != "" {
		args = append(args, "-probesize", probeSize)
	}
	args = append(args,
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate,avg_frame_rate,field_order",
		"-of", "default=noprint_wrappers=1",
		inputURL,
	)
	return args
}

func (a *LocalAdapter) runFPSProbe(ctx context.Context, inputURL string, timeout time.Duration, retry bool) (string, error) {
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ffprobeBin := a.FFprobeBin
	if strings.TrimSpace(ffprobeBin) == "" {
		ffprobeBin = "ffprobe"
	}

	args := a.buildFPSProbeArgs(inputURL, retry)

	// #nosec G204 -- ffprobe bin path is trusted
	cmd := exec.CommandContext(probeCtx, ffprobeBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", decorateProbeError(err, stderr.String())
	}

	output := strings.TrimSpace(string(out))
	if output == "" {
		return "", fmt.Errorf("empty output")
	}
	return output, nil
}

func (a *LocalAdapter) detectFPS(ctx context.Context, inputURL string) (int, string, error) {
	timeout := a.FPSProbeTimeout
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}

	output, err := a.runFPSProbe(ctx, inputURL, timeout, false)
	if err != nil {
		if !shouldRetryFPSProbe(err) {
			return 0, "", err
		}
		retryTimeout := timeout * 2
		if retryTimeout < 2500*time.Millisecond {
			retryTimeout = 2500 * time.Millisecond
		}
		if retryTimeout > 8*time.Second {
			retryTimeout = 8 * time.Second
		}
		retryOutput, retryErr := a.runFPSProbe(ctx, inputURL, retryTimeout, true)
		if retryErr != nil {
			return 0, "", fmt.Errorf("ffprobe failed after retry (primary=%v, retry=%w)", err, retryErr)
		}
		output = retryOutput
	}

	return parseFPSProbeOutput(output)
}

func parseFPS(output string) (int, error) {
	parts := strings.Split(output, "/")
	if len(parts) == 1 {
		val, err := strconv.Atoi(parts[0])
		return val, err
	}
	if len(parts) == 2 {
		num, err1 := strconv.Atoi(parts[0])
		den, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil || den == 0 {
			return 0, fmt.Errorf("invalid fractional fps: %s", output)
		}
		return int(math.Round(float64(num) / float64(den))), nil
	}

	return 0, fmt.Errorf("unrecognized fps format: %s", output)
}

func parseFPSProbeOutput(output string) (int, string, error) {
	var (
		rFPS       int
		avgFPS     int
		fieldOrder string
	)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "r_frame_rate":
			if parsed, err := parseFPS(val); err == nil && parsed > 0 {
				rFPS = parsed
			}
		case "avg_frame_rate":
			if parsed, err := parseFPS(val); err == nil && parsed > 0 {
				avgFPS = parsed
			}
		case "field_order":
			fieldOrder = strings.ToLower(val)
		}
	}

	fps, basis, ok := chooseBestFPS(rFPS, avgFPS, fieldOrder)
	if !ok {
		return 0, "", fmt.Errorf("no usable fps values (r_frame_rate=%d avg_frame_rate=%d field_order=%s)", rFPS, avgFPS, fieldOrder)
	}
	return fps, basis, nil
}

func chooseBestFPS(rFPS, avgFPS int, fieldOrder string) (int, string, bool) {
	if rFPS <= 0 && avgFPS <= 0 {
		return 0, "", false
	}
	if rFPS > 0 && avgFPS > 0 {
		if rFPS >= (avgFPS*2 - 1) {
			return rFPS, "r_frame_rate_field_rate", true
		}
		if isInterlacedFieldOrder(fieldOrder) && rFPS > avgFPS {
			return rFPS, "r_frame_rate_interlaced", true
		}
		return avgFPS, "avg_frame_rate", true
	}
	if rFPS > 0 {
		return rFPS, "r_frame_rate", true
	}
	return avgFPS, "avg_frame_rate", true
}

func isInterlacedFieldOrder(fieldOrder string) bool {
	switch strings.ToLower(strings.TrimSpace(fieldOrder)) {
	case "tt", "bb", "tb", "bt":
		return true
	default:
		return false
	}
}
