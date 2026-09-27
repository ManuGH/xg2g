// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgvInspector_Basic(t *testing.T) {
	rawArgs := []string{
		"-hide_banner",
		"-protocol_whitelist", "crypto,http,https,tcp,tls",
		"-i", "http://receiver:8001/1:0:19:1:1:C00000:0:0:0:",
		"-c:v", "copy",
		"-c:a", "aac",
		"-f", "hls",
	}

	ai := Argv(rawArgs)
	assert.True(t, ai.HasFlag("-protocol_whitelist"))
	assert.False(t, ai.HasFlag("-missing_flag"))
	assert.Equal(t, "crypto,http,https,tcp,tls", ai.Flag("-protocol_whitelist"))
	assert.Equal(t, "copy", ai.Flag("-c:v"))
	assert.Equal(t, "aac", ai.Flag("-c:a"))
	assert.Equal(t, "hls", ai.Flag("-f"))

	// Ordering: -protocol_whitelist must appear before -i
	assert.True(t, ai.Before("-protocol_whitelist", "-i"))
	assert.False(t, ai.Before("-i", "-protocol_whitelist"))

	ai.AssertFlag(t, "-c:v", "copy")
	ai.AssertContains(t, "crypto,http")
	ai.AssertNotContains(t, "nonexistent-arg")
	ai.AssertOrder(t, "-protocol_whitelist", "-i")
}

func TestArgvInspector_LiveMPEGTS_Plan(t *testing.T) {
	adapter := NewTestAdapter(t)
	spec := NewTestStreamSpec("sid-live-ts", "http://receiver:8001/1:0:19:1:1:C00000:0:0:0:", ports.ModeLive, "mpegts")

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	ai := Argv(args)

	// Invariant 1: Protocol whitelist must be set and appear before the input
	ai.AssertFlag(t, "-protocol_whitelist", "crypto,http,https,tcp,tls")
	ai.AssertOrder(t, "-protocol_whitelist", "-i")

	// Invariant 2: Output format must be HLS
	ai.AssertFlag(t, "-f", "hls")

	// Invariant 3: Segment type must be mpegts for mpegts container
	ai.AssertFlag(t, "-hls_segment_type", "mpegts")

	// Invariant 4: Video and Audio stream mappings are present
	ai.AssertContains(t, "0:v:0?")
	ai.AssertContains(t, "0:a:0?")
}

func TestArgvInspector_LiveFMP4_Plan(t *testing.T) {
	adapter := NewTestAdapter(t)
	spec := NewTestStreamSpec("sid-live-fmp4", "http://receiver:8001/1:0:19:1:1:C00000:0:0:0:", ports.ModeLive, "fmp4")

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	ai := Argv(args)

	// Invariant 1: Segment type must be fmp4
	ai.AssertFlag(t, "-hls_segment_type", "fmp4")

	// Invariant 2: Output format is HLS
	ai.AssertFlag(t, "-f", "hls")

	// Invariant 3: Init segment filename must be specified for fMP4
	assert.NotEmpty(t, ai.Flag("-hls_fmp4_init_filename"), "fMP4 HLS must declare init segment filename")
}

func TestArgvInspector_DVRWindow_Calculation(t *testing.T) {
	// With 45 minute DVR window and 2 second segments: list size should accommodate ~1350 segments
	adapter := NewTestAdapter(t, WithDVR(45*time.Minute), WithSegmentDuration(2))
	spec := NewTestStreamSpec("sid-dvr", "http://receiver:8001/1:0:19:1:1:C00000:0:0:0:", ports.ModeLive, "mpegts")

	args, err := adapter.buildArgs(context.Background(), spec, spec.Source.ID)
	require.NoError(t, err)

	ai := Argv(args)

	// List size flag must be set and large enough for DVR
	listSizeStr := ai.Flag("-hls_list_size")
	require.NotEmpty(t, listSizeStr)
	assert.NotEqual(t, "6", listSizeStr, "DVR window must expand list size far beyond live default 6")
}
