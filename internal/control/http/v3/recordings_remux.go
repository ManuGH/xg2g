package v3

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/ManuGH/xg2g/internal/control/vod"
)

// StreamInfo moved to github.com/ManuGH/xg2g/internal/control/vod

// ProbeStreams: DEPRECATED/REMOVED. Use s.vodManager.Probe()
// inferBitDepth: DEPRECATED/REMOVED type logic.

// RemuxStrategy represents the remux approach to use
type RemuxStrategy string

const (
	StrategyDefault     RemuxStrategy = "default"     // copy/copy or copy/transcode
	StrategyFallback    RemuxStrategy = "fallback"    // alternate flags for timestamp issues
	StrategyTranscode   RemuxStrategy = "transcode"   // full transcode (HEVC or remux failed)
	StrategyUnsupported RemuxStrategy = "unsupported" // fail fast
)

// RemuxDecision contains the strategy and ffmpeg arguments
type RemuxDecision struct {
	Strategy RemuxStrategy
	Args     []string
	Reason   string
}

func FindKeyframeStart(ctx context.Context, ffprobeBin, inputPath string) (string, error) {
	cmd := exec.CommandContext(ctx, ffprobeBin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "frame=pkt_pts_time,pict_type",
		"-of", "csv=p=0",
		"-read_intervals", "%+10",
		inputPath,
	)

	out, err := cmd.Output()
	if err != nil {
		return "1", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) >= 2 {
			timestamp := parts[0]
			frameType := parts[1]
			if frameType == "I" {
				if timestamp != "" && !strings.HasPrefix(timestamp, "-") {
					return timestamp, nil
				}
			}
		}
	}
	return "1", nil
}

func ComputeAudioDelayMs(info *vod.StreamInfo) int {
	return 0
}

var (
	ErrNonMonotonousDTS = errors.New("non-monotonous dts")
	ErrInvalidDuration  = errors.New("invalid duration")
	ErrTimestampUnset   = errors.New("timestamp unset")
)

// ErrFFmpegStalled is defined in recordings.go
