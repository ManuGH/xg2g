// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package scan

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/stretchr/testify/require"
)

// measuredColdProbeWorstCase is the slowest complete probe observed across 10
// encrypted broadcast channels on an idle receiver (median 6.1s). The default
// budget must clear it with room to spare, because a real scan run adds tuner
// contention on top.
const measuredColdProbeWorstCase = 11 * time.Second

func TestDefaultProbeBudgetCoversColdTune(t *testing.T) {
	require.Greater(t, defaultProbeTimeout, measuredColdProbeWorstCase,
		"defaultProbeTimeout must exceed the measured worst-case cold probe (%s); "+
			"a tighter budget kills slow-but-healthy channels mid-probe and records them "+
			"as failures, which then sit behind the %s failureRetryWindow",
		measuredColdProbeWorstCase, failureRetryWindow)

	require.Less(t, defaultProbeTimeout, extendedProbeTimeout,
		"the default budget must stay below the extended one so the two remain distinct tiers")
}

// The budget is only meaningful if it is actually enforced: a probe that
// outlasts it must be cancelled rather than allowed to run on.
func TestProbeAttemptIsCancelledAtBudget(t *testing.T) {
	m := NewManager(NewMemoryStore(), "", nil)

	budget := 60 * time.Millisecond
	m.probeFn = func(ctx context.Context, _ string, _ infra.ProbeOptions) (*vod.StreamInfo, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			return &vod.StreamInfo{Container: "ts"}, nil
		}
	}

	start := time.Now()
	_, err := m.runProbeAttempt(context.Background(), "http://receiver.example:8001/1:0:1:ABC",
		infra.ProbeOptions{}, budget)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded), "expected deadline error, got %v", err)
	require.Less(t, elapsed, 2*time.Second, "probe should be cancelled at the budget, not run on")
}

// A probe that finishes inside the budget must return its result untouched.
func TestProbeAttemptSucceedsWithinBudget(t *testing.T) {
	m := NewManager(NewMemoryStore(), "", nil)

	m.probeFn = func(_ context.Context, _ string, _ infra.ProbeOptions) (*vod.StreamInfo, error) {
		time.Sleep(10 * time.Millisecond)
		return &vod.StreamInfo{
			Container: "ts",
			Video:     vod.VideoStreamInfo{CodecName: "h264", Width: 1920, Height: 1080},
			Audio:     vod.AudioStreamInfo{CodecName: "ac3"},
		}, nil
	}

	res, err := m.runProbeAttempt(context.Background(), "http://receiver.example:8001/1:0:1:ABC",
		infra.ProbeOptions{}, time.Second)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, "h264", res.Video.CodecName)
	require.Equal(t, "ac3", res.Audio.CodecName)
}
