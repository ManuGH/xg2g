package ffmpeg

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/rs/zerolog"
)

// testAdapterOpt is an option for newTestAdapter.
type testAdapterOpt func(*LocalAdapter)

// newTestAdapter creates a LocalAdapter with sensible test defaults.
// Tests should call this instead of NewLocalAdapter directly to reduce
// boilerplate. Use with* options to override specific fields.
func newTestAdapter(t testing.TB, opts ...testAdapterOpt) *LocalAdapter {
	t.Helper()
	a := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// withFFprobeBin sets the ffprobe binary path.
func withFFprobeBin(bin string) testAdapterOpt {
	return func(a *LocalAdapter) { a.FFprobeBin = bin }
}

// withVaapiDevice sets the VAAPI device path and creates an empty encoder set.
func withVaapiDevice(dev string) testAdapterOpt {
	return func(a *LocalAdapter) {
		a.VaapiDevice = dev
		if a.vaapiEncoders == nil {
			a.vaapiEncoders = make(map[string]bool)
		}
	}
}

// withVaapiEncoders presets the available VAAPI encoders.
func withVaapiEncoders(encoders map[string]bool) testAdapterOpt {
	return func(a *LocalAdapter) { a.vaapiEncoders = encoders }
}

// withNVENCEncoders presets the available NVENC encoders.
func withNVENCEncoders(encoders map[string]bool) testAdapterOpt {
	return func(a *LocalAdapter) { a.nvencEncoders = encoders }
}

// withStreamProbeFn sets the stream probe function.
func withStreamProbeFn(fn func(context.Context, string) (*vod.StreamInfo, error)) testAdapterOpt {
	return func(a *LocalAdapter) { a.streamProbeFn = fn }
}

// withFPSProbeFn sets the FPS probe function.
func withFPSProbeFn(fn func(context.Context, string) (int, string, error)) testAdapterOpt {
	return func(a *LocalAdapter) { a.fpsProbeFn = fn }
}

// withSafariRuntimeProbeTimeout sets the Safari runtime probe timeout.
func withSafariRuntimeProbeTimeout(d time.Duration) testAdapterOpt {
	return func(a *LocalAdapter) { a.SafariRuntimeProbeTimeout = d }
}

// withDVRWindow sets the DVR window duration.
func withDVRWindow(d time.Duration) testAdapterOpt {
	return func(a *LocalAdapter) { a.DVRWindow = d }
}

// withSegmentSeconds sets the segment duration in seconds.
func withSegmentSeconds(sec int) testAdapterOpt {
	return func(a *LocalAdapter) { a.SegmentSeconds = sec }
}

// withLogBuf redirects the adapter's logger to the given buffer.
func withLogBuf(buf *bytes.Buffer) testAdapterOpt {
	return func(a *LocalAdapter) { a.Logger = zerolog.New(buf) }
}

// withNilLogger disables logging for the adapter.
func withNilLogger() testAdapterOpt {
	return func(a *LocalAdapter) { a.Logger = zerolog.New(io.Discard) }
}

// h264TSProgressive returns a stream probe function that returns a standard
// progressive h264 TS stream info.
func h264TSProgressive() func(context.Context, string) (*vod.StreamInfo, error) {
	return func(_ context.Context, _ string) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: "ts",
			Video: vod.VideoStreamInfo{
				CodecName:  "h264",
				Interlaced: false,
			},
		}, nil
	}
}

// h264TSInterlaced returns a stream probe function that returns an interlaced
// h264 TS stream info.
func h264TSInterlaced() func(context.Context, string) (*vod.StreamInfo, error) {
	return func(_ context.Context, _ string) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: "ts",
			Video: vod.VideoStreamInfo{
				CodecName:  "h264",
				Interlaced: true,
			},
		}, nil
	}
}

// specOpt is an option for newLiveSpec.
type specOpt func(*ports.StreamSpec)

// newLiveSpec creates a StreamSpec with common live-streaming defaults.
// Tests with non-default profiles, sources, or modes should use with* options.
func newLiveSpec(sessionID string, opts ...specOpt) ports.StreamSpec {
	s := ports.StreamSpec{
		SessionID: sessionID,
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Source: ports.StreamSource{
			ID:   "http://example.com/stream",
			Type: ports.SourceURL,
		},
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}

// withProfile sets the profile on a StreamSpec.
func withProfile(p model.ProfileSpec) specOpt {
	return func(s *ports.StreamSpec) { s.Profile = p }
}

// withTunerSource sets the source to a tuner source with the given ID.
func withTunerSource(id string) specOpt {
	return func(s *ports.StreamSpec) {
		s.Source = ports.StreamSource{
			ID:   id,
			Type: ports.SourceTuner,
		}
	}
}

// withSourceURL sets the source to a URL source.
func withSourceURL(url string) specOpt {
	return func(s *ports.StreamSpec) {
		s.Source = ports.StreamSource{
			ID:   url,
			Type: ports.SourceURL,
		}
	}
}

// withFormat overrides the stream format.
func withFormat(f ports.StreamFormat) specOpt {
	return func(s *ports.StreamSpec) { s.Format = f }
}

// withMode overrides the stream mode.
func withMode(m ports.StreamMode) specOpt {
	return func(s *ports.StreamSpec) { s.Mode = m }
}

// withQuality overrides the quality profile.
func withQuality(q ports.QualityProfile) specOpt {
	return func(s *ports.StreamSpec) { s.Quality = q }
}

// common test profile shortcuts

// safariProfile returns a Safari runtime probe profile with TS container.
func safariProfile() model.ProfileSpec {
	return model.ProfileSpec{
		Name:           "safari",
		TranscodeVideo: true,
		Container:      "mpegts",
		AudioBitrateK:  192,
	}
}

// safariFMP4Profile returns a Safari runtime probe profile with fMP4 container.
func safariFMP4Profile() model.ProfileSpec {
	return model.ProfileSpec{
		Name:           "safari",
		TranscodeVideo: true,
		Container:      "fmp4",
		AudioBitrateK:  192,
	}
}

// safariHQ25Profile returns a Safari HQ25 runtime probe profile.
func safariHQ25Profile() model.ProfileSpec {
	return model.ProfileSpec{
		Name:                "safari",
		PolicyModeHint:      ports.RuntimeModeHQ25,
		EffectiveModeSource: ports.RuntimeModeSourceResolve,
		TranscodeVideo:      true,
		Container:           "mpegts",
		AudioBitrateK:       192,
	}
}

// highProfile returns a high-quality passthrough profile.
func highProfile() model.ProfileSpec {
	return model.ProfileSpec{
		Name:           "high",
		TranscodeVideo: false,
		AudioBitrateK:  192,
	}
}

// copyProfile returns a default copy profile.
func copyProfile() model.ProfileSpec {
	return model.ProfileSpec{
		TranscodeVideo: false,
		AudioBitrateK:  192,
	}
}

// vaapiH264Profile returns a VAAPI H264 transcoding profile.
func vaapiH264Profile() model.ProfileSpec {
	return model.ProfileSpec{
		TranscodeVideo: true,
		HWAccel:        "vaapi",
		VideoCodec:     "h264",
		VideoQP:        20,
		Deinterlace:    true,
		VideoMaxRateK:  20000,
		VideoBufSizeK:  40000,
		AudioBitrateK:  192,
	}
}

// vaapiHEVCProfile returns a VAAPI HEVC transcoding profile.
func vaapiHEVCProfile() model.ProfileSpec {
	return model.ProfileSpec{
		TranscodeVideo: true,
		HWAccel:        "vaapi",
		VideoCodec:     "hevc",
		VideoQP:        20,
		VideoMaxRateK:  20000,
		VideoBufSizeK:  40000,
		AudioBitrateK:  192,
	}
}

// indexOf returns the index of target in args, or -1.
func indexOf(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}

// valueAfter returns the argument immediately after the given flag, or false.
func valueAfter(args []string, flag string) (string, bool) {
	idx := indexOf(args, flag)
	if idx < 0 || idx+1 >= len(args) {
		return "", false
	}
	return args[idx+1], true
}
