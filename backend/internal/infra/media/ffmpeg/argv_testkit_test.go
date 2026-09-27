// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ArgvInspector provides fluent assertions and inspection over FFmpeg command-line argument lists.
type ArgvInspector struct {
	args []string
}

// Argv wraps an FFmpeg argument slice into an ArgvInspector.
func Argv(args []string) *ArgvInspector {
	return &ArgvInspector{args: args}
}

// Raw returns the underlying argument slice.
func (ai *ArgvInspector) Raw() []string {
	return ai.args
}

// Index returns the index of the first occurrence of target, or -1 if not found.
func (ai *ArgvInspector) Index(target string) int {
	for i, a := range ai.args {
		if a == target {
			return i
		}
	}
	return -1
}

// HasArg returns true if target is in the argument list.
func (ai *ArgvInspector) HasArg(target string) bool {
	return ai.Index(target) >= 0
}

// HasFlag returns true if flag is present in the argument list.
func (ai *ArgvInspector) HasFlag(flag string) bool {
	return ai.HasArg(flag)
}

// Flag returns the value following flag, or empty string if not found.
func (ai *ArgvInspector) Flag(flag string) string {
	idx := ai.Index(flag)
	if idx < 0 || idx+1 >= len(ai.args) {
		return ""
	}
	return ai.args[idx+1]
}

// Flags returns all values following flag in the argument list (for repeated flags like -vf or -map).
func (ai *ArgvInspector) Flags(flag string) []string {
	var vals []string
	for i := 0; i < len(ai.args)-1; i++ {
		if ai.args[i] == flag {
			vals = append(vals, ai.args[i+1])
		}
	}
	return vals
}

// Before returns true if flagA appears earlier than flagB in the argument list.
func (ai *ArgvInspector) Before(flagA, flagB string) bool {
	idxA := ai.Index(flagA)
	idxB := ai.Index(flagB)
	if idxA < 0 || idxB < 0 {
		return false
	}
	return idxA < idxB
}

// Joined returns arguments formatted as a single command-line string.
func (ai *ArgvInspector) Joined() string {
	return strings.Join(ai.args, " ")
}

// Assertions

// AssertFlag asserts that flag exists and is followed by expectedValue.
func (ai *ArgvInspector) AssertFlag(t testing.TB, flag, expectedValue string) {
	t.Helper()
	actual := ai.Flag(flag)
	require.NotEmpty(t, actual, "Flag %q was not found in arguments: %s", flag, ai.Joined())
	assert.Equal(t, expectedValue, actual, "Flag %q value mismatch in: %s", flag, ai.Joined())
}

// AssertContains asserts that arg or substring appears in the argument list.
func (ai *ArgvInspector) AssertContains(t testing.TB, target string) {
	t.Helper()
	for _, a := range ai.args {
		if strings.Contains(a, target) {
			return
		}
	}
	t.Fatalf("Target %q not found anywhere in arguments:\n%s", target, ai.Joined())
}

// AssertNotContains asserts that target does NOT appear in the argument list.
func (ai *ArgvInspector) AssertNotContains(t testing.TB, target string) {
	t.Helper()
	for _, a := range ai.args {
		if strings.Contains(a, target) {
			t.Fatalf("Target %q unexpectedly found in arguments: %s", target, ai.Joined())
		}
	}
}

// AssertOrder asserts that flagA appears strictly before flagB.
func (ai *ArgvInspector) AssertOrder(t testing.TB, flagA, flagB string) {
	t.Helper()
	idxA := ai.Index(flagA)
	idxB := ai.Index(flagB)
	require.True(t, idxA >= 0, "Flag %q not found", flagA)
	require.True(t, idxB >= 0, "Flag %q not found", flagB)
	assert.True(t, idxA < idxB, "Expected %q (at %d) to appear before %q (at %d)", flagA, idxA, flagB, idxB)
}

// Test Adapter Factory

// AdapterOption configures a test LocalAdapter.
type AdapterOption func(*LocalAdapter)

func WithDVR(d time.Duration) AdapterOption {
	return func(a *LocalAdapter) {
		a.DVRWindow = d
	}
}

func WithSegmentDuration(sec int) AdapterOption {
	return func(a *LocalAdapter) {
		a.SegmentSeconds = sec
	}
}

// NewTestAdapter creates a pre-configured LocalAdapter with sane defaults for unit tests.
func NewTestAdapter(t testing.TB, opts ...AdapterOption) *LocalAdapter {
	t.Helper()
	adapter := NewLocalAdapterWithConfig(
		"ffmpeg",
		"ffprobe",
		t.TempDir(),
		nil,
		zerolog.New(io.Discard),
		"",
		"",
		0,
		0,
		false,
		2*time.Second,
		6,
		0,
		0,
		"",
		LoadAdapterConfig("", ""),
	)
	for _, opt := range opts {
		opt(adapter)
	}
	return adapter
}

// Helper to construct a standard StreamSpec for testing
func NewTestStreamSpec(sessionID, sourceURL string, mode ports.StreamMode, container string) ports.StreamSpec {
	return ports.StreamSpec{
		SessionID: sessionID,
		Mode:      mode,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Source: ports.StreamSource{
			ID:   sourceURL,
			Type: ports.SourceURL,
		},
		Profile: ports.ProfileSpec{
			Name:           "test_profile",
			TranscodeVideo: false,
			AudioBitrateK:  320,
			Container:      container,
			PolicyModeHint: ports.RuntimeModeCopy,
		},
	}
}
