package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

// decodeProbeCodec pairs an input codec with the software encoder that can
// synthesise a sample of it, so the probe needs nothing but FFmpeg itself.
type decodeProbeCodec struct {
	Codec   string
	Encoder string
}

// decodeProbeCodecs covers what actually arrives from a DVB tuner or a
// recording: H.264 on most HD channels, MPEG-2 on the remaining SD ones, HEVC
// on UHD. Anything else falls back to software decode, which is exactly the
// behaviour these probes protect.
var decodeProbeCodecs = []decodeProbeCodec{
	{Codec: "h264", Encoder: "libx264"},
	{Codec: "hevc", Encoder: "libx265"},
	{Codec: "mpeg2video", Encoder: "mpeg2video"},
}

// PreflightVAAPIDecode proves which input codecs this GPU can decode, instead
// of assuming that a device with a working encoder also decodes everything.
//
// The full-GPU pipeline pins -hwaccel_output_format vaapi, so a decoder the
// driver does not provide is not a graceful software fallback - it fails the
// session before the first frame, the same way an unsupported rate-control mode
// does. Probing keeps that failure on the startup path, where it costs a log
// line, rather than on a viewer's zap.
func (d *Detector) PreflightVAAPIDecode() {
	if d.decodeChecked {
		return
	}
	d.decodeChecked = true

	if d.VaapiDevice == "" {
		return
	}

	capabilities := make(map[string]bool, len(decodeProbeCodecs))
	for _, probe := range decodeProbeCodecs {
		err := d.probeVAAPIDecode(probe)
		capabilities[probe.Codec] = err == nil
		event := d.Logger.Info()
		if err != nil {
			// Not a host defect: GPUs legitimately decode a subset, and the
			// point of asking is to find out which.
			event = d.Logger.Info().Str("reason", err.Error())
		}
		event.
			Str("input_codec", probe.Codec).
			Bool("supported", err == nil).
			Msg("vaapi preflight: hardware decode probe")
	}

	hardware.SetVAAPIDecodeCapabilities(capabilities)
}

func (d *Detector) probeVAAPIDecode(probe decodeProbeCodec) error {
	if d.decodeProbeFn != nil {
		return d.decodeProbeFn(context.Background(), probe.Codec)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tempDir, err := os.MkdirTemp("", "xg2g-decode-probe-*")
	if err != nil {
		return fmt.Errorf("mktemp decode probe: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	samplePath := filepath.Join(tempDir, "sample.mkv")
	encodeArgs := []string{
		"-y",
		"-hide_banner",
		"-f", "lavfi",
		"-i", "testsrc2=duration=0.4:size=1280x720:rate=25",
		"-c:v", probe.Encoder,
		"-frames:v", "5",
		samplePath,
	}
	if probe.Encoder == "libx264" || probe.Encoder == "libx265" {
		encodeArgs = append(encodeArgs[:len(encodeArgs)-1], "-preset", "ultrafast", samplePath)
	}
	if _, err := runProfileBenchmarkCommand(ctx, d.BinPath, encodeArgs); err != nil {
		// No sample means no verdict. Report it as unsupported rather than
		// guessing: the safe direction is software decode.
		return fmt.Errorf("could not synthesise %s sample: %w", probe.Codec, err)
	}

	// The decode side must mirror production exactly: -hwaccel_output_format
	// vaapi is what turns a missing decoder from a fallback into a failure.
	decodeArgs := []string{
		"-hide_banner",
		"-vaapi_device", d.VaapiDevice,
		"-hwaccel", "vaapi",
		"-hwaccel_output_format", "vaapi",
		"-i", samplePath,
		"-f", "null", "-",
	}
	_, err = runProfileBenchmarkCommand(ctx, d.BinPath, decodeArgs)
	return err
}
