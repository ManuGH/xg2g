// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputProbeInput_FMP4PrependsInit(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.mp4")
	if err := os.WriteFile(initPath, []byte("moovdata"), 0o600); err != nil {
		t.Fatalf("write init: %v", err)
	}
	seg := filepath.Join(dir, "seg_000000.m4s")
	if err := os.WriteFile(seg, []byte("moofmdat"), 0o600); err != nil {
		t.Fatalf("write seg: %v", err)
	}

	input, extra := outputProbeInput(seg)

	want := "concat:file:" + filepath.ToSlash(initPath) + "|file:" + filepath.ToSlash(seg)
	if input != want {
		t.Fatalf("input = %q, want %q", input, want)
	}
	if len(extra) != 2 || extra[0] != "-protocol_whitelist" || extra[1] != "concat,file" {
		t.Fatalf("extra args = %v, want [-protocol_whitelist concat,file]", extra)
	}
}

// Negative control: a self-contained MPEG-TS segment must pass through
// untouched — prepending init.mp4 (or whitelisting concat) for TS would be
// wrong. If the fix ever over-reached to all segments this fails.
func TestOutputProbeInput_TSPassesThrough(t *testing.T) {
	seg := filepath.Join(t.TempDir(), "seg_000000.ts")
	if err := os.WriteFile(seg, []byte("tsdata"), 0o600); err != nil {
		t.Fatalf("write seg: %v", err)
	}

	input, extra := outputProbeInput(seg)

	if input != seg {
		t.Fatalf("input = %q, want bare segment %q", input, seg)
	}
	if extra != nil {
		t.Fatalf("extra args = %v, want nil for TS", extra)
	}
}

// Negative control: an fMP4 segment whose init.mp4 hasn't been written yet must
// degrade to the bare segment, not emit a concat input pointing at a missing
// file (which would fail differently). The probe simply returns its normal
// not-decodable error and the caller retries on the next segment.
func TestOutputProbeInput_FMP4WithoutInitFallsBack(t *testing.T) {
	seg := filepath.Join(t.TempDir(), "seg_000000.m4s")
	if err := os.WriteFile(seg, []byte("moofmdat"), 0o600); err != nil {
		t.Fatalf("write seg: %v", err)
	}

	input, extra := outputProbeInput(seg)

	if input != seg {
		t.Fatalf("input = %q, want bare segment fallback %q", input, seg)
	}
	if extra != nil {
		t.Fatalf("extra args = %v, want nil when init.mp4 absent", extra)
	}
}
