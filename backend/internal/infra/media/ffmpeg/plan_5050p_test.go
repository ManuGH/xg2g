// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
)

func liveTranscodeSpec(mode ports.RuntimeMode) ports.StreamSpec {
	s := ports.StreamSpec{Mode: ports.ModeLive, Format: ports.FormatHLS}
	s.Profile.TranscodeVideo = true
	s.Profile.Deinterlace = true
	s.Profile.EffectiveRuntimeMode = mode
	return s
}

// When promoted to HQ50, the output framerate must NOT be clamped (return 0 =
// no -r): the send_field bob deinterlace emits native 50fps and force_key_frames
// keeps segments aligned. Clamping would collapse motion back toward 25.
func TestTargetLiveOutputFPS_HQ50NoClamp(t *testing.T) {
	if got := targetLiveOutputFPS(liveTranscodeSpec(ports.RuntimeModeHQ50)); got != 0 {
		t.Fatalf("HQ50 output fps clamp = %d, want 0 (no clamp)", got)
	}
}

// Negative control: HQ25 stays at 25 fps. If this regresses, every interlaced
// transcode silently changes framerate.
func TestTargetLiveOutputFPS_HQ25Pins25(t *testing.T) {
	if got := targetLiveOutputFPS(liveTranscodeSpec(ports.RuntimeModeHQ25)); got != 25 {
		t.Fatalf("HQ25 output fps = %d, want 25", got)
	}
}

// HQ50 deinterlaces with send_field (one frame per field → 50p, smooth motion).
func TestDeinterlaceFilter_HQ50UsesSendField(t *testing.T) {
	a := &LocalAdapter{Logger: zerolog.Nop()}
	f := a.deinterlaceFilterForProfile(liveTranscodeSpec(ports.RuntimeModeHQ50))
	if !strings.Contains(f, "send_field") {
		t.Fatalf("HQ50 deinterlace = %q, want send_field (50p)", f)
	}
}

// Negative control: HQ25 deinterlaces with send_frame (25p), collapsing fields.
func TestDeinterlaceFilter_HQ25UsesSendFrame(t *testing.T) {
	a := &LocalAdapter{Logger: zerolog.Nop()}
	f := a.deinterlaceFilterForProfile(liveTranscodeSpec(ports.RuntimeModeHQ25))
	if !strings.Contains(f, "send_frame") {
		t.Fatalf("HQ25 deinterlace = %q, want send_frame (25p)", f)
	}
}

// The 50p promotion only applies to interlaced transcodes — progressive and
// copy/passthrough never promote (these return before the host benchmark is even
// consulted, so they are deterministic without benchmark state). Guards against
// over-reaching to non-interlaced or copy streams.
func TestShouldPromote50p_RequiresInterlacedTranscode(t *testing.T) {
	a := &LocalAdapter{Logger: zerolog.Nop()}

	progressive := liveTranscodeSpec(ports.RuntimeModeHQ25)
	progressive.Profile.Deinterlace = false
	if a.shouldPromoteInterlacedTo50p(progressive) {
		t.Fatal("progressive source must never promote to 50p")
	}

	copyStream := liveTranscodeSpec(ports.RuntimeModeHQ25)
	copyStream.Profile.TranscodeVideo = false
	if a.shouldPromoteInterlacedTo50p(copyStream) {
		t.Fatal("copy/passthrough must never promote to 50p")
	}

	vod := liveTranscodeSpec(ports.RuntimeModeHQ25)
	vod.Mode = ports.ModeRecording
	if a.shouldPromoteInterlacedTo50p(vod) {
		t.Fatal("non-live must never promote to 50p")
	}
}
