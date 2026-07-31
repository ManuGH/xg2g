package ffmpeg

import (
	"context"
	"strconv"
	"time"

	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
)

// rateControlProbeCandidates lists the non-default rate-control modes worth
// asking a driver about, per codec. VBR is absent on purpose: it is the
// implicit fallback every VAAPI driver supports, so a probe would only confirm
// that the encoder works at all - which the encoder preflight already proved.
// ICQ is absent on purpose too, see hardware.RateControlICQ.
var rateControlProbeCandidates = map[string][]string{
	"av1_vaapi":  {hardware.RateControlQVBR},
	"hevc_vaapi": {hardware.RateControlQVBR},
	"h264_vaapi": {hardware.RateControlQVBR},
}

// PreflightVAAPIRateControlModes proves which rate-control modes each verified
// VAAPI encoder actually accepts, instead of inferring them from the GPU
// vendor. A mode that fails here would otherwise fail at encoder-open in a live
// session, which costs the viewer the whole stream rather than some quality.
//
// Cost is one short synthetic encode per encoder and candidate mode - a handful
// of frames at 640x480, measured at roughly 60-90ms each on current hardware.
func (d *Detector) PreflightVAAPIRateControlModes() {
	if d.rateControlChecked {
		return
	}
	d.rateControlChecked = true

	if d.VaapiDevice == "" {
		return
	}

	capabilities := make(map[string]map[string]bool)
	for _, encoder := range []string{"av1_vaapi", "hevc_vaapi", "h264_vaapi"} {
		if !d.VaapiEncoderVerified(encoder) {
			continue
		}
		modes := make(map[string]bool)
		for _, mode := range rateControlProbeCandidates[encoder] {
			err := d.probeVAAPIRateControlMode(encoder, mode)
			modes[mode] = err == nil
			event := d.Logger.Info()
			if err != nil {
				// Not a failure of the host: most drivers implement a subset of
				// the modes, and finding that out is the point of the probe.
				event = d.Logger.Info().Str("reason", err.Error())
			}
			event.
				Str("encoder", encoder).
				Str("rc_mode", mode).
				Bool("supported", err == nil).
				Msg("vaapi preflight: rate control mode probe")
		}
		capabilities[encoder] = modes
	}

	hardware.SetVAAPIRateControlCapabilities(capabilities)

	options := make(map[string]map[string]bool)
	for _, encoder := range []string{"av1_vaapi", "hevc_vaapi", "h264_vaapi"} {
		if !d.VaapiEncoderVerified(encoder) {
			continue
		}
		err := d.probeVAAPIEncoderOption(encoder, "-bf", strconv.Itoa(vaapiBFrames))
		options[encoder] = map[string]bool{hardware.OptionBFrames: err == nil}
		event := d.Logger.Info()
		if err != nil {
			event = d.Logger.Info().Str("reason", err.Error())
		}
		event.
			Str("encoder", encoder).
			Str("option", hardware.OptionBFrames).
			Bool("supported", err == nil).
			Msg("vaapi preflight: encoder option probe")
	}
	hardware.SetVAAPIEncoderOptions(options)
}

// vaapiBFrames is the reorder depth requested where the encoder accepts it.
// Measured on an Intel AV1 encoder at fixed QP: -bf 7 spends 9.3% fewer bits
// than -bf 0 for the same quality, -bf 2 spends 7.0% fewer, and neither costs
// throughput (7.31x vs 7.05x realtime). The reorder delay this adds is about
// seven frames - 140ms at 50fps - against 2s segments and a client buffer of
// several seconds, so it is irrelevant for live.
const vaapiBFrames = 7

func (d *Detector) probeVAAPIEncoderOption(encoder string, option, value string) error {
	if d.encoderOptionProbeFn != nil {
		return d.encoderOptionProbeFn(context.Background(), encoder, option, value)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	args := []string{
		"-y",
		"-hide_banner",
		"-vaapi_device", d.VaapiDevice,
		"-f", "lavfi",
		"-i", "testsrc2=duration=0.4:size=1280x720:rate=25",
		"-vf", "format=" + vaapiProductionUploadFormat(encoder) + ",hwupload",
		"-c:v", encoder,
		option, value,
		"-b:v", "2000k",
		"-maxrate", "3000k",
		"-frames:v", "10",
		"-f", "null", "-",
	}
	_, err := runProfileBenchmarkCommand(ctx, d.BinPath, args)
	return err
}

func (d *Detector) probeVAAPIRateControlMode(encoder, mode string) error {
	if d.rateControlProbeFn != nil {
		return d.rateControlProbeFn(context.Background(), encoder, mode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Short, but at a production-like geometry on purpose. The question is "does
	// the driver accept this mode", which is answered when the encoder opens -
	// but some encoders fail small frames for unrelated reasons (hevc_vaapi on
	// iHD returns "internal encoding error" at 640x480), and a probe that cannot
	// tell a rejected mode from a broken probe silently gives away quality.
	args := []string{
		"-y",
		"-hide_banner",
		"-vaapi_device", d.VaapiDevice,
		"-f", "lavfi",
		"-i", "testsrc2=duration=0.2:size=1280x720:rate=25",
		"-vf", "format=" + vaapiProductionUploadFormat(encoder) + ",hwupload",
		"-c:v", encoder,
		"-rc_mode", mode,
		// QVBR refuses to open without a target bitrate ("Bitrate must be set
		// for QVBR RC mode"), so supply the same shape a live encode uses.
		"-b:v", "2000k",
		"-maxrate", "3000k",
		"-bufsize", "6000k",
		"-global_quality", "90",
		"-frames:v", "3",
		"-f", "null", "-",
	}
	_, err := runProfileBenchmarkCommand(ctx, d.BinPath, args)
	return err
}
