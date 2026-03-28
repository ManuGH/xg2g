package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProbeWithBin_RedactsCredentialsInFFprobeError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	script := "#!/bin/sh\n" +
		"echo 'http://user:secret@example.com:17999/1:0:19:EF75:3F9:1:C00000:0:0:0: Input/output error' 1>&2\n" +
		"exit 1\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}

	_, err := ProbeWithBin(context.Background(), scriptPath, "http://user:secret@example.com:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:")
	if err == nil {
		t.Fatal("expected error")
	}

	errText := err.Error()
	if strings.Contains(errText, "secret") || strings.Contains(errText, "user:") {
		t.Fatalf("expected credentials to be redacted, got %q", errText)
	}
	if !strings.Contains(errText, "http://example.com:17999/1:0:19:EF75:3F9:1:C00000:0:0:0:") {
		t.Fatalf("expected sanitized url in error, got %q", errText)
	}
}

func TestProbeWithOptions_AddsAnalyzeDurationAndProbeSize(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	argsPath := filepath.Join(tmpDir, "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\n' \"$@\" > \"" + argsPath + "\"\n" +
		"printf '{\"streams\":[{\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":1920,\"height\":1080}],\"format\":{\"duration\":\"1.0\",\"format_name\":\"mpegts\"}}'\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ffprobe: %v", err)
	}

	_, err := probeWithBinAndOptions(context.Background(), scriptPath, "http://example.com/stream", ProbeOptions{
		AnalyzeDuration: 15 * time.Second,
		ProbeSizeBytes:  8 << 20,
	})
	if err != nil {
		t.Fatalf("probe with options: %v", err)
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	argsText := string(argsBytes)
	if !strings.Contains(argsText, "-analyzeduration\n15000000\n") {
		t.Fatalf("expected analyzeduration override in args, got %q", argsText)
	}
	if !strings.Contains(argsText, "-probesize\n8388608\n") {
		t.Fatalf("expected probesize override in args, got %q", argsText)
	}
}
