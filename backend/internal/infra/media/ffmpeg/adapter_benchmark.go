package ffmpeg

import (
	"context"
	"fmt"
	playbackports "github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/ManuGH/xg2g/internal/infra/media/ffmpeg/capability"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"os"
	"os/exec"
	"strings"
	"time"
)

func profileBenchmarksForBackend(backend string) []string {
	switch backend {
	case "cpu":
		return startupProfilesToBenchmark
	case "vaapi", "nvenc":
		return []string{
			playbackports.BenchmarkProfileVideoH2641080P,
			playbackports.BenchmarkProfileVideoH2641080I,
			playbackports.BenchmarkProfileVideoH2641080I50,
			playbackports.BenchmarkProfileVideoH2642160P,
			playbackports.BenchmarkProfileVideoH2642160P50,
		}
	default:
		return nil
	}
}

// vaapiInterlacedProbeUploadFormat returns the hwupload pixel format the
// interlaced path-correctness probe must encode at so it verifies the SAME bit
// depth production drives the codec at (see encode_args.go: AV1 -> p010le 10-bit,
// H.264/HEVC -> nv12 8-bit). Probing nv12 for AV1 "verifies" an 8-bit interlaced
// path that is never served -- production always uploads AV1 as p010le.
func vaapiInterlacedProbeUploadFormat(encoder string) string {
	if normalizeRequestedCodec(encoder) == "av1" {
		return "p010le"
	}
	return "nv12"
}

func vaapiEncodeOnlyInterlacedCorrectnessFilter(encoder string) string {
	parts := []string{
		"setfield=tff",
		"bwdif=mode=send_field:parity=auto:deint=all",
	}
	if normalizeRequestedCodec(encoder) == "av1" {
		parts = append(parts, av1VAAPIGeometryPadFilter())
	}
	parts = append(parts, "format="+vaapiInterlacedProbeUploadFormat(encoder), "hwupload")
	return strings.Join(parts, ",")
}

func isAV1SignalStatsDecodeUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "doesn't support hardware accelerated av1 decoding") ||
		strings.Contains(msg, "error submitting packet to decoder")
}

func outputFileHasBytes(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func cpuProfileBenchmarkFilter(profileID string) string {
	switch strings.ToLower(strings.TrimSpace(profileID)) {
	case playbackports.BenchmarkProfileVideoH2641080I, playbackports.BenchmarkProfileVideoH2641080I50:
		return "setfield=tff,bwdif=mode=send_field:parity=auto:deint=all"
	default:
		return ""
	}
}

func vaapiProfileBenchmarkFilter(profileID string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profileID)) {
	case playbackports.BenchmarkProfileVideoH2641080P:
		return "format=nv12,hwupload", nil
	case playbackports.BenchmarkProfileVideoH2641080I50:
		return "format=nv12,setfield=tff,hwupload,deinterlace_vaapi", nil
	case playbackports.BenchmarkProfileVideoH2642160P:
		return "format=nv12,hwupload", nil
	case playbackports.BenchmarkProfileVideoH2642160P50:
		return "format=nv12,hwupload", nil
	case playbackports.BenchmarkProfileVideoH2641080I:
		return "format=nv12,setfield=tff,hwupload,deinterlace_vaapi", nil
	default:
		return "", fmt.Errorf("unsupported vaapi benchmark profile %q", profileID)
	}
}

func nvencProfileBenchmarkFilter(profileID string) string {
	switch strings.ToLower(strings.TrimSpace(profileID)) {
	case playbackports.BenchmarkProfileVideoH2641080I, playbackports.BenchmarkProfileVideoH2641080I50:
		return "setfield=tff,bwdif=mode=send_field:parity=auto:deint=all"
	default:
		return ""
	}
}

func profileBenchmarkInput(profileID string) string {
	switch strings.ToLower(strings.TrimSpace(profileID)) {
	case playbackports.BenchmarkProfileVideoH2642160P50:
		return "testsrc=duration=0.2:size=3840x2160:rate=50"
	case playbackports.BenchmarkProfileVideoH2642160P:
		return "testsrc=duration=0.2:size=3840x2160:rate=25"
	case playbackports.BenchmarkProfileVideoH2641080I50:
		return "testsrc=duration=0.2:size=1920x1080:rate=50"
	default:
		return "testsrc=duration=0.2:size=1920x1080:rate=25"
	}
}

func profileBenchmarkTimeout(profileID string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(profileID)) {
	case playbackports.BenchmarkProfileVideoH2642160P50:
		return 22 * time.Second
	case playbackports.BenchmarkProfileVideoH2642160P:
		return 18 * time.Second
	case playbackports.BenchmarkProfileVideoH2641080I50:
		return 15 * time.Second
	default:
		return 12 * time.Second
	}
}

func runProfileBenchmarkCommand(ctx context.Context, binPath string, args []string) (time.Duration, error) {
	start := time.Now()
	// #nosec G204 -- BinPath is trusted from config and args are fixed synthetic probes.
	cmd := exec.CommandContext(ctx, binPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("profile benchmark failed: %w (output: %s)", err, string(out))
	}
	return time.Since(start), nil
}

func (a *LocalAdapter) supportedHWCodecsLocal() []string {
	codecs := make([]string, 0, 3)
	for _, codec := range []string{"h264", "hevc", "av1"} {
		if a.detector.preferredHardwareBackendForCodec(codec) != profiles.GPUBackendNone {
			codecs = append(codecs, codec)
		}
	}
	return codecs
}

func (a *LocalAdapter) autoHWCodecsLocal() []string {
	codecs := make([]string, 0, 3)
	for _, codec := range []string{"h264", "hevc", "av1"} {
		for _, backend := range []profiles.GPUBackend{profiles.GPUBackendVAAPI, profiles.GPUBackendNVENC} {
			encoder, ok := capability.EncoderNameForBackend(codec, backend)
			if !ok {
				continue
			}
			if a.detector.hardwareEncoderAutoEligible(backend, encoder) {
				codecs = append(codecs, codec)
				break
			}
		}
	}
	return codecs
}

// Preflight delegators keep the adapter's external surface (bootstrap wiring)
// stable while encoder-capability detection lives in the Detector.
func (a *LocalAdapter) PreflightVAAPI() error       { return a.detector.PreflightVAAPI() }
func (a *LocalAdapter) PreflightNVENC() error       { return a.detector.PreflightNVENC() }
func (a *LocalAdapter) PreflightTranscodeProfiles() { a.detector.PreflightTranscodeProfiles() }

func (a *LocalAdapter) PreflightPathCorrectness() { a.detector.PreflightPathCorrectness() }
