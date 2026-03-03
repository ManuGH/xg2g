package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFFprobeBin_Explicit(t *testing.T) {
	t.Parallel()

	got := ResolveFFprobeBin("/custom/ffprobe", "/custom/ffmpeg")
	if got != "/custom/ffprobe" {
		t.Fatalf("expected explicit ffprobe bin, got %q", got)
	}
}

func TestResolveFFprobeBin_DeriveFromFFmpegBin_WhenPresent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ffmpegBin := filepath.Join(dir, "ffmpeg")
	ffprobeBin := filepath.Join(dir, "ffprobe")

	// Only the derived ffprobe path needs to exist for derivation.
	if err := os.WriteFile(ffprobeBin, []byte("stub"), 0o755); err != nil {
		t.Fatalf("write ffprobe stub: %v", err)
	}

	got := ResolveFFprobeBin("", ffmpegBin)
	if got != ffprobeBin {
		t.Fatalf("expected derived ffprobe bin %q, got %q", ffprobeBin, got)
	}
}

func TestResolveFFprobeBin_NoDerive_WhenNotAPath(t *testing.T) {
	t.Parallel()

	got := ResolveFFprobeBin("", "ffmpeg")
	if got != "" {
		t.Fatalf("expected empty (PATH fallback), got %q", got)
	}
}

func TestResolveFFprobeBin_NoDerive_WhenMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	got := ResolveFFprobeBin("", filepath.Join(dir, "ffmpeg"))
	if got != "" {
		t.Fatalf("expected empty (PATH fallback), got %q", got)
	}
}
