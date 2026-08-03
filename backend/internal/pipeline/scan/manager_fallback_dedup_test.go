// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package scan

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/stretchr/testify/require"
)

// recordingProber captures every URL probeWithFallbacks actually attempts.
func recordingProber(m *Manager, err error) *[]string {
	attempts := make([]string, 0, 4)
	m.probeFn = func(_ context.Context, probeURL string, _ infra.ProbeOptions) (*vod.StreamInfo, error) {
		attempts = append(attempts, probeURL)
		return nil, err
	}
	return &attempts
}

// A receiver serving its own streaming port resolves straight to port 8001, so
// the standard-port fallback would re-probe the URL that just failed. It must be
// skipped rather than attempted twice.
func TestProbeWithFallbacks_SkipsStandardPortWhenAlreadyAttempted(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	initial := "http://receiver.example:8001/" + serviceRef

	m := NewManager(NewMemoryStore(), "", nil)
	attempts := recordingProber(m, errors.New("probe failed"))

	_, _, err := m.probeWithFallbacks(
		context.Background(), serviceRef, initial, initial, infra.ProbeOptions{}, 50*time.Millisecond)
	require.Error(t, err)

	count := 0
	for _, a := range *attempts {
		if normalizeProbeURL(a) == normalizeProbeURL(initial) {
			count++
		}
	}
	require.Equal(t, 1, count,
		"port 8001 must be probed once, not re-attempted as its own fallback; attempts=%v", *attempts)
}

// When the initial probe targets a different port, the standard-port fallback is
// a genuinely new target and must still be attempted.
func TestProbeWithFallbacks_AttemptsStandardPortWhenDistinct(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	initial := "http://receiver.example:17999/" + serviceRef
	expectedFallback := "http://receiver.example:8001/" + serviceRef

	m := NewManager(NewMemoryStore(), "", nil)
	attempts := recordingProber(m, errors.New("probe failed"))

	_, _, err := m.probeWithFallbacks(
		context.Background(), serviceRef, initial, initial, infra.ProbeOptions{}, 50*time.Millisecond)
	require.Error(t, err)

	require.Contains(t, *attempts, expectedFallback,
		"a distinct standard-port target must still be probed; attempts=%v", *attempts)
}

// The warning must describe what actually happened. Announcing a port 8001
// attempt that is then skipped made scan runs read as if the fallback had
// carried the probe, which is what this regression guards.
func TestProbeWithFallbacks_LogReflectsWhetherFallbackRan(t *testing.T) {
	cases := []struct {
		name        string
		initialPort string
		wantSubstr  string
		notWant     string
	}{
		{
			name:        "already on standard port: announces the skip",
			initialPort: "8001",
			wantSubstr:  "port 8001 already attempted, skipping",
			notWant:     "attempting port 8001 fallback",
		},
		{
			name:        "distinct port: announces the attempt",
			initialPort: "17999",
			wantSubstr:  "attempting port 8001 fallback",
			notWant:     "already attempted, skipping",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log.Configure(log.Config{Level: "debug", Output: &buf, Service: "test"})
			t.Cleanup(func() { log.Configure(log.Config{Level: "info", Service: "test"}) })

			serviceRef := "1:0:1:ABC"
			initial := "http://receiver.example:" + tc.initialPort + "/" + serviceRef

			m := NewManager(NewMemoryStore(), "", nil)
			recordingProber(m, errors.New("probe failed"))

			_, _, err := m.probeWithFallbacks(
				context.Background(), serviceRef, initial, initial, infra.ProbeOptions{}, 50*time.Millisecond)
			require.Error(t, err)

			out := buf.String()
			require.Contains(t, out, tc.wantSubstr)
			require.NotContains(t, out, tc.notWant)
		})
	}
}

// The standard-port fallback stays a recovery path: when the first attempt
// succeeds it must not be reached at all.
func TestProbeWithFallbacks_NoFallbackWhenInitialSucceeds(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	initial := "http://receiver.example:8001/" + serviceRef

	m := NewManager(NewMemoryStore(), "", nil)
	attempts := make([]string, 0, 2)
	m.probeFn = func(_ context.Context, probeURL string, _ infra.ProbeOptions) (*vod.StreamInfo, error) {
		attempts = append(attempts, probeURL)
		return &vod.StreamInfo{
			Container: "ts",
			Video:     vod.VideoStreamInfo{CodecName: "h264", Width: 1920, Height: 1080},
			Audio:     vod.AudioStreamInfo{CodecName: "ac3"},
		}, nil
	}

	res, usedURL, err := m.probeWithFallbacks(
		context.Background(), serviceRef, initial, initial, infra.ProbeOptions{}, 50*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, initial, usedURL)
	require.Equal(t, []string{initial}, attempts, "a successful first probe must not trigger any fallback")
}
