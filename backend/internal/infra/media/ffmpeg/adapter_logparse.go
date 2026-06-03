package ffmpeg

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	metricsgpu "github.com/ManuGH/xg2g/internal/metrics/gpu"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/rs/zerolog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func scanFFmpegLogTokens(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) == 0 {
		if atEOF {
			return 0, nil, nil
		}
		return 0, nil, nil
	}
	for i, b := range data {
		if b != '\n' && b != '\r' {
			continue
		}
		advance = i + 1
		if b == '\r' && advance < len(data) && data[advance] == '\n' {
			advance++
		}
		return advance, bytes.TrimRight(data[:i], "\r\n"), nil
	}
	if atEOF {
		return len(data), bytes.TrimRight(data, "\r\n"), nil
	}
	return 0, nil, nil
}

func parseFFmpegFrameCount(line string) (int, bool) {
	_, after, ok := strings.Cut(line, "frame=")
	if !ok {
		return 0, false
	}
	rest := strings.TrimLeft(after, " ")
	if rest == "" {
		return 0, false
	}
	count := 0
	digits := 0
	for digits < len(rest) {
		ch := rest[digits]
		if ch < '0' || ch > '9' {
			break
		}
		count = count*10 + int(ch-'0')
		digits++
	}
	if digits == 0 {
		return 0, false
	}
	return count, true
}

func (a *LocalAdapter) recordRuntimeDiagnostics(handle ports.RunHandle, rawLine string, sanitizedLine string) {
	if handle == "" {
		return
	}

	nowUnix := time.Now().Unix()
	frame, hasFrame := parseFFmpegFrameCount(rawLine)
	fps, hasFPS := parseFFmpegFloatValue(rawLine, "fps")
	drops, hasDrops := parseFFmpegIntValue(rawLine, "drop_frames")
	if !hasDrops {
		drops, hasDrops = parseFFmpegIntValue(rawLine, "drop")
	}
	duplicates, hasDuplicates := parseFFmpegIntValue(rawLine, "dup_frames")
	if !hasDuplicates {
		duplicates, hasDuplicates = parseFFmpegIntValue(rawLine, "dup")
	}
	speed, hasSpeed := parseFFmpegFloatValue(rawLine, "speed")
	isProgressLine := hasFrame || hasFPS || hasDrops || hasDuplicates || hasSpeed

	var corruptDecodedFrame bool
	var warningLine string
	if !isProgressLine {
		lower := strings.ToLower(strings.TrimSpace(sanitizedLine))
		if strings.Contains(lower, "corrupt decoded frame") {
			corruptDecodedFrame = true
			warningLine = sanitizedLine
		} else if !isFFmpegProgressLine(lower) && (summarizeFFmpegFailureLine(lower) != "" || looksLikeFFmpegWarning(lower)) {
			warningLine = sanitizedLine
		}
	}

	if !isProgressLine && warningLine == "" {
		return
	}

	var changed bool
	a.mu.Lock()
	diagnostics := a.runtimeDiagnostics[handle]
	if hasFrame {
		diagnostics.FrameCount = frame
		changed = true
	}
	if hasFPS {
		diagnostics.FPS = fps
		changed = true
	}
	if hasDrops {
		diagnostics.DropFrames = drops
		changed = true
	}
	if hasDuplicates {
		diagnostics.DupFrames = duplicates
		changed = true
	}
	if hasSpeed {
		diagnostics.Speed = speed
		changed = true
	}
	if corruptDecodedFrame {
		diagnostics.CorruptDecodedFrames++
	}
	if warningLine != "" {
		diagnostics.LastWarning = trimForLog(warningLine, 240)
		changed = true
	}
	if changed {
		diagnostics.UpdatedAtUnix = nowUnix
		if a.runtimeDiagnostics == nil {
			a.runtimeDiagnostics = make(map[ports.RunHandle]ports.RuntimeDiagnostics)
		}
		a.runtimeDiagnostics[handle] = diagnostics
	}
	a.mu.Unlock()
}

func parseFFmpegIntValue(line string, key string) (int, bool) {
	raw, ok := parseFFmpegValue(line, key)
	if !ok {
		return 0, false
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseFFmpegFloatValue(line string, key string) (float64, bool) {
	raw, ok := parseFFmpegValue(line, key)
	if !ok {
		return 0, false
	}
	raw = strings.TrimSuffix(raw, "x")
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseFFmpegValue(line string, key string) (string, bool) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	idx := strings.Index(line, key+"=")
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimLeft(line[idx+len(key)+1:], " ")
	if rest == "" {
		return "", false
	}
	end := 0
	for end < len(rest) {
		ch := rest[end]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '+' || ch == 'x' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return "", false
	}
	return rest[:end], true
}

func extractStartupSegmentPath(line string) (string, bool) {
	if !strings.Contains(line, "Opening ") || !strings.Contains(line, " for writing") {
		return "", false
	}
	start := strings.IndexAny(line, `'"`)
	if start < 0 || start+1 >= len(line) {
		return "", false
	}
	quote := line[start]
	endRel := strings.IndexByte(line[start+1:], quote)
	if endRel < 0 {
		return "", false
	}
	path := line[start+1 : start+1+endRel]
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "seg_") {
		return "", false
	}
	if strings.HasSuffix(base, ".m4s") || strings.HasSuffix(base, ".ts") {
		return path, true
	}
	return "", false
}

func summarizeFFmpegFailureLine(line string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(lower, "non-existing pps"),
		strings.Contains(lower, "non-existing sps"),
		strings.Contains(lower, "could not find codec parameters for stream") && strings.Contains(lower, "unspecified size"),
		strings.Contains(lower, "could not write header (incorrect codec parameters ?)"),
		strings.Contains(lower, "dimensions not set"):
		return "copy output missing codec parameters"
	case strings.Contains(lower, "stream ends prematurely"):
		return "upstream stream ended prematurely"
	case strings.Contains(lower, "error opening input files"),
		strings.Contains(lower, "error opening input file"),
		strings.Contains(lower, "error opening input:"):
		if strings.Contains(lower, "input/output error") {
			return "upstream input/output error"
		}
		return "failed to open upstream input"
	case strings.Contains(lower, "invalid data found when processing input"):
		return "invalid upstream input data"
	}
	return ""
}

// looksLikeEncoderOpenFailure matches ffmpeg lines reporting a hardware encoder
// failing to open/initialize — e.g. feeding a 10-bit surface to an 8-bit-profile
// VAAPI encoder. ffmpeg emits these without the "vaapi"/"nvenc" token on the
// same line (the codec name sits in a separate component-prefix line), so the
// backend-specific matchers miss them and no GPU->CPU demotion fires. The call
// site already gates which backend's session this applies to, so a plain
// substring match here is safe.
func looksLikeEncoderOpenFailure(lower string) bool {
	for _, kw := range []string{
		"could not open encoder",
		"cannot open encoder",
		"failed to open encoder",
		"error while opening encoder",
		"no usable encoding profile",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isVAAPIRuntimeFailureLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "vaapi") && looksLikeFFmpegWarning(lower) {
		return true
	}
	if looksLikeEncoderOpenFailure(lower) {
		return true
	}
	definitiveKeywords := []string{
		"libva error",
		"no usable encoding entrypoint",
		"failed to end picture",
		"failed to sync surface",
		"failed to export surface",
		"failed to upload",
		"hardware device reference is required",
		"va_create",
	}
	for _, keyword := range definitiveKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	if (strings.Contains(lower, "hwupload") || strings.Contains(lower, "renderd128")) && looksLikeFFmpegWarning(lower) {
		return true
	}
	return false
}

func isNVENCRuntimeFailureLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "nvenc") && looksLikeFFmpegWarning(lower) {
		return true
	}
	if looksLikeEncoderOpenFailure(lower) {
		return true
	}
	definitiveKeywords := []string{
		"cannot load libnvidia-encode.so.1",
		"driver does not support the required nvenc api version",
		"no capable devices found",
		"no nvenc capable devices found",
		"openencode session failed",
		"provided device doesn't support required nvenc features",
		"cannot init encoder",
		"nvidia",
	}
	for _, keyword := range definitiveKeywords {
		if strings.Contains(lower, keyword) && looksLikeFFmpegWarning(lower) {
			return true
		}
	}
	return false
}

func (a *LocalAdapter) recordVAAPIRuntimeFailure(sessionID, failureLine string) {
	if !hardware.IsVAAPIReady() {
		return
	}
	failures, demoted := hardware.RecordVAAPIRuntimeFailure()
	metricsgpu.RecordRuntimeEncodeFailure("vaapi")
	event := a.Logger.Warn().
		Str("session_id", sessionID).
		Int("vaapi_runtime_failures", failures)
	if failureLine != "" {
		event = event.Str("ffmpeg_log", failureLine)
	}
	if demoted {
		metricsgpu.RecordRuntimeDemotion("vaapi")
		metricsgpu.SetEncoderVerified("h264_vaapi", false)
		metricsgpu.SetEncoderVerified("hevc_vaapi", false)
		metricsgpu.SetEncoderVerified("av1_vaapi", false)
		event.Msg("vaapi runtime failure threshold reached; gpu demoted to cpu fallback")
		return
	}
	event.Msg("recorded vaapi runtime failure")
}

func (a *LocalAdapter) recordNVENCRuntimeFailure(sessionID, failureLine string) {
	if !hardware.IsNVENCReady() {
		return
	}
	failures, demoted := hardware.RecordNVENCRuntimeFailure()
	metricsgpu.RecordRuntimeEncodeFailure("nvenc")
	event := a.Logger.Warn().
		Str("session_id", sessionID).
		Int("nvenc_runtime_failures", failures)
	if failureLine != "" {
		event = event.Str("ffmpeg_log", failureLine)
	}
	if demoted {
		metricsgpu.RecordRuntimeDemotion("nvenc")
		metricsgpu.SetEncoderVerified("h264_nvenc", false)
		metricsgpu.SetEncoderVerified("hevc_nvenc", false)
		metricsgpu.SetEncoderVerified("av1_nvenc", false)
		event.Msg("nvenc runtime failure threshold reached; gpu demoted to cpu fallback")
		return
	}
	event.Msg("recorded nvenc runtime failure")
}

func ffmpegLogLevel(line string) zerolog.Level {
	lower := strings.ToLower(strings.TrimSpace(line))
	if lower == "" {
		return zerolog.DebugLevel
	}
	if isFFmpegProgressLine(lower) {
		return zerolog.DebugLevel
	}
	if summarizeFFmpegFailureLine(lower) != "" || looksLikeFFmpegWarning(lower) {
		return zerolog.WarnLevel
	}
	return zerolog.InfoLevel
}

func isFFmpegProgressLine(lower string) bool {
	switch {
	case strings.HasPrefix(lower, "frame="),
		strings.HasPrefix(lower, "fps="),
		strings.HasPrefix(lower, "stream_"),
		strings.HasPrefix(lower, "bitrate="),
		strings.HasPrefix(lower, "total_size="),
		strings.HasPrefix(lower, "out_time_us="),
		strings.HasPrefix(lower, "out_time_ms="),
		strings.HasPrefix(lower, "out_time="),
		strings.HasPrefix(lower, "dup_frames="),
		strings.HasPrefix(lower, "drop_frames="),
		strings.HasPrefix(lower, "speed="),
		strings.HasPrefix(lower, "progress="):
		return true
	case strings.Contains(lower, " opening '") && strings.Contains(lower, "' for writing"):
		return true
	case strings.Contains(lower, "opening \"") && strings.Contains(lower, "\" for writing"):
		return true
	case strings.Contains(lower, "press [q] to stop"):
		return true
	default:
		return false
	}
}

func looksLikeFFmpegWarning(lower string) bool {
	keywords := []string{
		" error",
		"error ",
		"failed",
		"invalid",
		"non-existing",
		"no frame",
		"decode_slice_header",
		"corrupt",
		"unable to",
		"could not",
		"connection refused",
		"broken pipe",
	}
	for _, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

func summarizeProcessExit(procErr error) string {
	if procErr == nil {
		return ""
	}
	var exitErr *exec.ExitError
	if errors.As(procErr, &exitErr) {
		return fmt.Sprintf("process exit code %d", exitErr.ExitCode())
	}
	return "process exited unexpectedly"
}
