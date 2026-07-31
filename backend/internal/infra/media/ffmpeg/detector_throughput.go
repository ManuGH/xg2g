package ffmpeg

import (
	"context"
	"fmt"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

const (
	// throughputProbeSeconds is how much content each measurement encodes. Two
	// seconds is one HLS segment: long enough that process startup does not
	// dominate the number, short enough that three codecs cost about a second
	// of boot on a healthy host.
	throughputProbeSeconds = 2

	// throughputProbeGeometry documents what the number means. Live broadcast
	// is 1080i25 bobbed to 50p, and that is the load that decides whether this
	// host can carry 50p at all - measuring anything else would produce a
	// figure nobody may act on.
	throughputProbeGeometry = "1920x1080i25->50p"
)

// PreflightVAAPIThroughput measures what this host actually sustains, instead
// of inferring capacity from core count.
//
// The core-count heuristic was wrong here in the most consequential way: a
// four-core host classified as "medium", which locked AV1 out of the auto-codec
// policy entirely, while the same host encoded 1080i25 to 50p AV1 at more than
// seven times realtime because the work runs on the GPU. Core count says
// nothing about a machine whose transcode path is a fixed-function block.
func (d *Detector) PreflightVAAPIThroughput() {
	if d.throughputChecked {
		return
	}
	d.throughputChecked = true

	if d.VaapiDevice == "" {
		return
	}

	measurements := make(map[string]hardware.EncoderThroughput)
	for _, encoder := range []string{"av1_vaapi", "hevc_vaapi", "h264_vaapi"} {
		if !d.VaapiEncoderVerified(encoder) {
			continue
		}
		realtimeX, err := d.probeVAAPIThroughput(encoder)
		if err != nil {
			d.Logger.Info().
				Err(err).
				Str("encoder", encoder).
				Msg("vaapi preflight: throughput probe failed")
			continue
		}
		codec := normalizeRequestedCodec(encoder)
		measurements[codec] = hardware.EncoderThroughput{
			RealtimeX: realtimeX,
			Geometry:  throughputProbeGeometry,
		}
		d.Logger.Info().
			Str("encoder", encoder).
			Str("geometry", throughputProbeGeometry).
			Float64("realtime_x", realtimeX).
			Msg("vaapi preflight: throughput measured")
	}

	if len(measurements) == 0 {
		return
	}
	hardware.SetVAAPIThroughput(measurements)
}

func (d *Detector) probeVAAPIThroughput(encoder string) (float64, error) {
	if d.throughputProbeFn != nil {
		return d.throughputProbeFn(context.Background(), encoder)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Deliberately the production filter chain, not a bare encode: on this
	// hardware the encoder alone runs at 5.4x while the same encode behind a
	// software filter chain manages 1.6x. A capacity number that ignores the
	// filters describes a pipeline nobody runs.
	filter := fmt.Sprintf(
		"format=%s,hwupload,deinterlace_vaapi=rate=field,scale_vaapi=format=%s",
		vaapiProductionUploadFormat(encoder),
		vaapiThroughputScaleFormat(encoder),
	)
	args := []string{
		"-hide_banner",
		"-vaapi_device", d.VaapiDevice,
		"-f", "lavfi",
		"-i", fmt.Sprintf("testsrc2=duration=%d:size=1920x1080:rate=25", throughputProbeSeconds),
		"-vf", filter,
		"-c:v", encoder,
		"-b:v", "12000k",
		"-maxrate", "12000k",
		"-bufsize", "24000k",
		"-f", "null", "-",
	}
	elapsed, err := runProfileBenchmarkCommand(ctx, d.BinPath, args)
	if err != nil {
		return 0, err
	}
	if elapsed <= 0 {
		return 0, fmt.Errorf("throughput probe returned no elapsed time")
	}
	return float64(throughputProbeSeconds) / elapsed.Seconds(), nil
}

func vaapiThroughputScaleFormat(encoder string) string {
	if normalizeRequestedCodec(encoder) == "av1" {
		return "p010"
	}
	return "nv12"
}
