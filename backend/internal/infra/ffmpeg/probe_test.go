package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeExecutableScript(t *testing.T, path string, content string) {
	t.Helper()

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0o755); err != nil {
		t.Fatalf("write temp script: %v", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		t.Fatalf("rename temp script: %v", err)
	}
}

func TestProbeWithBin_RedactsCredentialsInFFprobeError(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	script := "#!/bin/sh\n" +
		"echo 'http://user:secret@example.com:17999/1:0:19:EF75:3F9:1:C00000:0:0:0: Input/output error' 1>&2\n" +
		"exit 1\n"
	writeExecutableScript(t, scriptPath, script)

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
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	argsPath := filepath.Join(tmpDir, "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\n' \"$@\" > \"" + argsPath + "\"\n" +
		"printf '{\"streams\":[{\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":1920,\"height\":1080}],\"format\":{\"duration\":\"1.0\",\"format_name\":\"mpegts\"}}'\n"
	writeExecutableScript(t, scriptPath, script)

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

func TestProbeWithBin_ParsesFormatBitrate(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	script := "#!/bin/sh\n" +
		"printf '{\"streams\":[{\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":1920,\"height\":1080,\"field_order\":\"tt\",\"avg_frame_rate\":\"25/1\",\"r_frame_rate\":\"50/1\"},{\"codec_type\":\"audio\",\"codec_name\":\"aac\",\"sample_rate\":\"48000\",\"channels\":6,\"channel_layout\":\"5.1(side)\",\"bit_rate\":\"384000\"}],\"format\":{\"duration\":\"1.0\",\"format_name\":\"mpegts\",\"bit_rate\":\"12345678\"}}'\n"
	writeExecutableScript(t, scriptPath, script)

	info, err := ProbeWithBin(context.Background(), scriptPath, "http://example.com/stream")
	if err != nil {
		t.Fatalf("probe with bitrate: %v", err)
	}

	if info.BitrateKbps != 12346 {
		t.Fatalf("expected rounded bitrate 12346 kbps, got %d", info.BitrateKbps)
	}
	if info.Video.FPS != 25 || info.Video.SignalFPS != 50 {
		t.Fatalf("expected avg fps=25 and signal fps=50, got video=%#v", info.Video)
	}
	if !info.Video.Interlaced || info.Video.FieldOrder != "tt" {
		t.Fatalf("expected interlaced field order truth, got video=%#v", info.Video)
	}
	if info.Audio.SampleRate != 48000 || info.Audio.Channels != 6 || info.Audio.ChannelLayout != "5.1(side)" || info.Audio.BitrateKbps != 384 {
		t.Fatalf("expected parsed audio truth, got audio=%#v", info.Audio)
	}
}
