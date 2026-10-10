// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"fmt"
	"strings"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

// av1Ladder is the optional two-rung live AV1 ladder for native iOS clients.
// The regular rung keeps the profile's rate; a second rung at the same
// resolution and GOP runs at lowRateK. Both come from one decode and one filter
// chain (split after the last filter), so their segments are keyframe-aligned
// and AVPlayer can switch at any segment boundary.
//
// Without a second rung AVPlayer has no alternate to fall back to: over a
// high-latency mobile link its variant selection fails (HLS-FASB -15514),
// it re-requests the first segment several times per second and startup
// stalls for tens of seconds or never completes. On fast links it switches
// to the regular rung once it has measured the bandwidth.
type av1Ladder struct {
	lowRateK int
}

func (l av1Ladder) enabled() bool { return l.lowRateK > 0 }

func isIOSNativeSpec(spec ports.StreamSpec) bool {
	switch strings.ToLower(strings.TrimSpace(spec.ClientFamily)) {
	case "ios_native", "ios_safari":
		return true
	default:
		return false
	}
}

// planAV1Ladder decides whether a live output gets the low rung. It only
// applies to the VAAPI fMP4 HLS path that writes var_stream_map renditions.
func (a *LocalAdapter) planAV1Ladder(spec ports.StreamSpec, codec codecPlan) av1Ladder {
	low := a.Config.AV1LadderLowRateK
	switch {
	case low <= 0,
		spec.Mode != ports.ModeLive,
		!spec.Profile.TranscodeVideo,
		spec.Profile.EnableABR,
		a.LowLatencyHLS,
		a.useCMAFSegmenter(spec),
		!isIOSNativeSpec(spec),
		normalizeRequestedCodec(codec.resolvedCodec) != "av1",
		!strings.EqualFold(strings.TrimSpace(spec.Profile.Container), "fmp4"),
		!codec.useHW || codec.hwBackend != profiles.GPUBackendVAAPI,
		// Rate-controlled encodes only: under CQP both rungs would spend alike.
		spec.Profile.VideoQP > 0,
		spec.Profile.VideoMaxRateK <= 0,
		low >= spec.Profile.VideoMaxRateK:
		return av1Ladder{}
	}
	return av1Ladder{lowRateK: low}
}

// ladderVideoVariants lists the low rung first: AVPlayer starts on the first
// variant of the master playlist, so a constrained link starts at once and a
// fast one switches up after its first bandwidth sample.
const ladderVideoVariants = "v:0,agroup:audio v:1,agroup:audio,default:yes"

// audioLayout rewrites the audio selection into the rendition layout the
// ladder needs: both video rungs share one audio group. A single selected
// track becomes a one-entry audio group instead of being muxed into the video.
func (l av1Ladder) audioLayout(sel liveAudioSelection) liveAudioSelection {
	if sel.IsMultiAudio {
		audio := make([]string, 0, 4)
		for _, entry := range strings.Fields(sel.VarStreamMap) {
			if strings.HasPrefix(entry, "a:") {
				audio = append(audio, entry)
			}
		}
		sel.VarStreamMap = ladderVideoVariants + " " + strings.Join(audio, " ")
		return sel
	}
	lang := strings.TrimSpace(sel.Language)
	if lang == "" {
		lang = "und"
	}
	sel.IsMultiAudio = true
	sel.VarStreamMap = ladderVideoVariants + " a:0,agroup:audio,language:" + lang + ",default:yes"
	return sel
}

// videoMaps replaces the single "-map 0:v:0?" so the video rungs stay the
// first output streams: v:0 is the low rung, v:1 the regular one. -map may
// precede -filter_complex, which ffmpeg applies as a global option.
func (l av1Ladder) videoMaps() []string {
	if !l.enabled() {
		return []string{"-map", "0:v:0?"}
	}
	return []string{"-map", "[vlo]", "-map", "[vhi]"}
}

// appendVideoInput emits the shared filter chain. Without the ladder it is the
// usual -vf; with it the chain ends in a split feeding both rungs.
func (l av1Ladder) appendVideoInput(args []string, filterChain string) []string {
	if !l.enabled() {
		return append(args, "-vf", filterChain)
	}
	return append(args, "-filter_complex", "[0:v:0]"+filterChain+",split=2[vlo][vhi]")
}

// appendLowRungRateArgs overrides the rate control for v:0 only. ffmpeg keeps
// the last matching per-stream option, so these must follow the unindexed
// rate arguments that the regular rung keeps.
func (l av1Ladder) appendLowRungRateArgs(args []string, cfg AdapterConfig) []string {
	if !l.enabled() {
		return args
	}
	target := l.lowRateK
	if cfg.GPUVendor == string(hardware.GPUVendorAMD) {
		// Same ring-stall headroom the regular AV1 rung gets on AMD.
		target = max((l.lowRateK*3)/4, 1)
	}
	return append(args,
		"-b:v:0", fmt.Sprintf("%dk", target),
		"-maxrate:v:0", fmt.Sprintf("%dk", l.lowRateK),
		"-bufsize:v:0", fmt.Sprintf("%dk", l.lowRateK*2),
	)
}
