package ffmpeg

import (
	"context"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

const experimentalInterlacedVAAPICodecsEnv = "XG2G_EXPERIMENTAL_ALLOW_UNVERIFIED_INTERLACED_VAAPI_CODECS"

type inputPlan struct {
	args     []string
	inputURL string

	// authURL preserves the original input URL with userinfo credentials
	// before they are stripped from inputURL.  Probe functions (ffprobe,
	// warmup HTTP GET, safari runtime probe) use this URL so they can
	// authenticate against protected sources even though the main ffmpeg
	// -i argument has been sanitised to prevent credential leakage via
	// /proc/<pid>/cmdline.
	authURL string
}

type codecPlan struct {
	resolvedCodec string
	useHW         bool
	hwBackend     profiles.GPUBackend
	fullVAAPI     bool
	preInputArgs  []string
	pathID        string
}

type outputPlan struct {
	args             []string
	effectiveProfile ports.ProfileSpec

	// cmafSegment marks the LL-HLS pipe mode: ffmpeg emits one fragmented
	// MP4 stream on stdout and the in-process cmaf segmenter produces the
	// session artifacts instead of the hls muxer.
	cmafSegment      bool
	cmafTargetDurSec int
}

type finalizedPlan struct {
	args             []string
	effectiveProfile ports.ProfileSpec
	pathID           string

	cmafSegment      bool
	cmafTargetDurSec int
}

type liveSegmentLayout struct {
	segmentDurationSec     int
	initSegmentDurationSec int
	listSize               int
}

func (a *LocalAdapter) buildArgsWithPlan(ctx context.Context, spec ports.StreamSpec, inputURL string) (finalizedPlan, error) {
	inputPhase, err := a.planInput(spec, inputURL)
	if err != nil {
		return finalizedPlan{}, err
	}
	if spec.Mode == ports.ModeLive {
		// Pass the original (pre-sanitisation) URL so that any probe calls
		// inside FinalizePlan (e.g. safari runtime probe) can authenticate
		// against protected sources.  Service-ref extraction uses the same
		// host/path/query structure either way, so the credentials in the
		// userinfo portion do not interfere.
		spec = a.FinalizePlan(ctx, spec, inputPhase.authURL)
	}

	codecPhase, err := a.planCodec(spec)
	if err != nil {
		return finalizedPlan{}, err
	}

	args := append([]string{}, codecPhase.preInputArgs...)
	args = append(args, inputPhase.args...)
	args = append(args, "-progress", "pipe:2")
	result := finalizedPlan{
		args:             args,
		effectiveProfile: spec.Profile,
		pathID:           codecPhase.pathID,
	}

	if spec.Mode == ports.ModeLive {
		liveOutput, err := a.planLiveOutput(ctx, spec, inputPhase, codecPhase)
		if err != nil {
			return finalizedPlan{}, err
		}
		result.args = append(result.args, liveOutput.args...)
		result.effectiveProfile = liveOutput.effectiveProfile
		result.cmafSegment = liveOutput.cmafSegment
		result.cmafTargetDurSec = liveOutput.cmafTargetDurSec
	}

	return result, nil
}
