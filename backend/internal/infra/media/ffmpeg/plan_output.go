package ffmpeg

import (
	"context"
	"fmt"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	playbackports "github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func (a *LocalAdapter) planLiveOutput(ctx context.Context, spec ports.StreamSpec, input inputPlan, codec codecPlan) (outputPlan, error) {
	layout, err := a.planLiveSegmentLayout(spec)
	if err != nil {
		return outputPlan{}, err
	}
	spec.Profile.VideoCodec = codec.resolvedCodec
	if a.shouldPromoteInterlacedTo50p(spec) {
		// Host benchmarked strong enough to sustain the doubled encode load, so
		// keep the full motion of the interlaced source instead of collapsing 50
		// fields to 25p. The existing send_field deinterlace + targetLiveOutputFPS
		// (HQ50) then emit true 50p. EffectiveRuntimeMode wins over PolicyModeHint.
		spec.Profile.EffectiveRuntimeMode = ports.RuntimeModeHQ50
	}
	// Use the pre-sanitisation URL so ffprobe/warmup probes can authenticate
	// against protected sources.  adjustLiveFPSForRuntimeServiceOverride only
	// extracts the service ref from the URL structure and works correctly with
	// either version.
	probeURL := input.authURL
	if probeURL == "" {
		probeURL = input.inputURL
	}
	fps := a.resolveLiveFPS(ctx, spec, probeURL)
	fps = a.adjustLiveFPSForRuntimeServiceOverride(spec, input.inputURL, fps)
	targetOutputFPS := targetLiveOutputFPS(spec)
	gopFPS := fps
	if targetOutputFPS > 0 {
		gopFPS = targetOutputFPS
	}
	gop := gopFPS * layout.segmentDurationSec

	out := outputPlan{effectiveProfile: spec.Profile}
	out.args = append(out.args,
		"-map", "0:v:0?",
		"-map", "0:a:0?",
	)
	if targetOutputFPS > 0 {
		out.args = append(out.args, "-r", strconv.Itoa(targetOutputFPS))
	}

	out.args = a.buildLiveVideoOutputArgs(out.args, spec, input.inputURL, codec, gop, layout.segmentDurationSec)
	out.args = appendLiveVideoContainerTags(out.args, spec, codec.resolvedCodec)
	out.args = appendLiveAudioArgs(out.args, spec)
	out.args = a.appendLiveHLSArgs(out.args, spec, layout)
	out.args = append(out.args, a.prepareLiveOutputPath(spec.SessionID))

	// One source of truth: record the hwAccel the emitted argv actually reflects,
	// so the profile-derived predicted FFmpeg plan matches execution. The planner
	// can downgrade full VAAPI -> encode-only (unverified interlaced HEVC/AV1,
	// forced AV1) or fall back off the GPU; without this writeback the prediction
	// (profile.HWAccel) would diverge from the real argv, causing a spurious
	// plan_mismatch warning. VideoCodec is already synced to codec.resolvedCodec.
	out.effectiveProfile.HWAccel = resolvedExecutedHWAccel(codec)

	return out, nil
}

// shouldPromoteInterlacedTo50p reports whether an interlaced live transcode
// should preserve full 50-field motion (50p) instead of the safe-default 25p.
// We only promote when the host's startup benchmark for the 1080i50 profile came
// back "strong" (encoder fast enough to sustain the doubled framerate). Moderate
// or weak hosts stay at 25p so a 50p transcode can never overload them. The
// interlaced flag comes from the scan (Deinterlace=true), so this is capability-
// gated, not guesswork.
func (a *LocalAdapter) shouldPromoteInterlacedTo50p(spec ports.StreamSpec) bool {
	if spec.Mode != ports.ModeLive || spec.Format != ports.FormatHLS {
		return false
	}
	if !spec.Profile.TranscodeVideo || !spec.Profile.Deinterlace {
		return false
	}
	class := playbackprofile.BenchmarkClassForProfile(
		hardware.SnapshotHostBenchmark(),
		playbackports.BenchmarkProfileVideoH2641080I50,
	)
	return class == "strong"
}

func (a *LocalAdapter) planLiveSegmentLayout(spec ports.StreamSpec) (liveSegmentLayout, error) {
	layout := liveSegmentLayout{
		segmentDurationSec: a.SegmentSeconds,
		listSize:           10,
	}
	if shouldUseShortFMP4StartupSegments(spec) && layout.segmentDurationSec > safariDirtyHLSTimeSec {
		layout.segmentDurationSec = safariDirtyHLSTimeSec
		layout.initSegmentDurationSec = min(safariDirtyHLSInitTimeSec, layout.segmentDurationSec)
	}
	if layout.segmentDurationSec <= 0 {
		return liveSegmentLayout{}, fmt.Errorf("invalid hls segment seconds: %d", layout.segmentDurationSec)
	}
	if a.DVRWindow > 0 {
		layout.listSize = max(int(math.Ceil(a.DVRWindow.Seconds()/float64(layout.segmentDurationSec))), 3)
	}
	return layout, nil
}

func shouldUseShortFMP4StartupSegments(spec ports.StreamSpec) bool {
	if spec.Mode != ports.ModeLive || !spec.Profile.TranscodeVideo {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return false
	}

	switch profiles.NormalizeRequestedProfileID(spec.Profile.Name) {
	case profiles.ProfileSafariDirty,
		profiles.ProfileSafariHEVCHW,
		profiles.ProfileSafariHEVCHWLL,
		profiles.ProfileAV1HW:
		// Native iOS fMP4 transcodes benefit from a denser startup GOP cadence.
		// The default 6-second layout makes first attach visibly sluggish.
		return true
	default:
		return false
	}
}

func (a *LocalAdapter) buildLiveVideoOutputArgs(args []string, spec ports.StreamSpec, inputURL string, codec codecPlan, gop, segmentDurationSec int) []string {
	if !spec.Profile.TranscodeVideo && !usesLegacyCPUDefaults(spec, codec.resolvedCodec) {
		return a.buildCopyVideoArgs(args, spec, inputURL)
	}
	if codec.useHW {
		switch codec.hwBackend {
		case profiles.GPUBackendVAAPI:
			if codec.fullVAAPI {
				return a.buildVaapiVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
			}
			return a.buildVaapiEncodeOnlyVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		case profiles.GPUBackendNVENC:
			return a.buildNVENCVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
		}
	}
	return a.buildCPUVideoArgs(args, spec, codec.resolvedCodec, gop, segmentDurationSec)
}

func appendLiveAudioArgs(args []string, spec ports.StreamSpec) []string {
	audioBitrate := "192k"
	if spec.Profile.AudioBitrateK > 0 {
		audioBitrate = fmt.Sprintf("%dk", spec.Profile.AudioBitrateK)
	}
	return append(args,
		"-c:a", "aac",
		"-b:a", audioBitrate,
		"-ac", "2",
		"-ar", "48000",
		"-sn",
		"-f", "hls",
	)
}

func appendLiveVideoContainerTags(args []string, spec ports.StreamSpec, outputCodec string) []string {
	if !strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		return args
	}
	if !strings.EqualFold(strings.TrimSpace(outputCodec), "hevc") {
		return args
	}
	return append(args, "-tag:v", "hvc1")
}

func (a *LocalAdapter) appendLiveHLSArgs(args []string, spec ports.StreamSpec, layout liveSegmentLayout) []string {
	segmentType := "mpegts"
	sessionDir := ports.SessionHLSDir(a.HLSRoot, spec.SessionID)
	segmentFilename := filepath.Join(sessionDir, "seg_%06d.ts")
	if strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4") {
		segmentType = "fmp4"
		segmentFilename = filepath.Join(sessionDir, "seg_%06d.m4s")
	}
	args = append(args,
		"-hls_time", strconv.Itoa(layout.segmentDurationSec),
		"-hls_list_size", strconv.Itoa(layout.listSize),
		"-hls_flags", "delete_segments+append_list+independent_segments+program_date_time",
		"-hls_segment_type", segmentType,
		"-hls_segment_filename", segmentFilename,
	)
	if segmentType == "fmp4" {
		args = append(args, "-hls_fmp4_init_filename", "init.mp4")
	}
	if layout.initSegmentDurationSec > 0 {
		args = append(args, "-hls_init_time", strconv.Itoa(layout.initSegmentDurationSec))
	}
	return args
}

func (a *LocalAdapter) prepareLiveOutputPath(sessionID string) string {
	outputPath := filepath.Join(ports.SessionHLSDir(a.HLSRoot, sessionID), "index.m3u8")
	_ = os.MkdirAll(filepath.Dir(outputPath), 0755) // #nosec G301
	if markerPath := ports.SessionFirstFrameMarkerPath(a.HLSRoot, sessionID); markerPath != "" {
		_ = os.Remove(markerPath)
	}
	a.Logger.Info().
		Str("session_id", sessionID).
		Str("startup_phase", "output_dir_ready").
		Str("output_path", outputPath).
		Msg("output directory ready")
	return outputPath
}
