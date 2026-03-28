package ffmpeg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeWithBin_AddsProtocolWhitelistForRemoteInput(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	argsPath := filepath.Join(tmpDir, "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\n' \"$@\" > \"" + argsPath + "\"\n" +
		"printf '{\"streams\":[{\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":1920,\"height\":1080}],\"format\":{\"duration\":\"1.0\",\"format_name\":\"mpegts\"}}'\n"
	writeExecutableScript(t, scriptPath, script)

	_, err := ProbeWithBin(context.Background(), scriptPath, "http://user:secret@example.com/playlist.m3u8")
	if err != nil {
		t.Fatalf("probe with remote input: %v", err)
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	argsText := string(argsBytes)
	if !strings.Contains(argsText, "-protocol_whitelist\ncrypto,http,https,tcp,tls\n") {
		t.Fatalf("expected protocol whitelist in args, got %q", argsText)
	}
}

func TestProbeWithBin_OmitsProtocolWhitelistForLocalFile(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "fake-ffprobe.sh")
	argsPath := filepath.Join(tmpDir, "args.txt")
	script := "#!/bin/sh\n" +
		"printf '%s\n' \"$@\" > \"" + argsPath + "\"\n" +
		"printf '{\"streams\":[{\"codec_type\":\"video\",\"codec_name\":\"h264\",\"width\":1920,\"height\":1080}],\"format\":{\"duration\":\"1.0\",\"format_name\":\"mpegts\"}}'\n"
	writeExecutableScript(t, scriptPath, script)

	_, err := ProbeWithBin(context.Background(), scriptPath, "/tmp/example.ts")
	if err != nil {
		t.Fatalf("probe with local file: %v", err)
	}

	argsBytes, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	argsText := string(argsBytes)
	if strings.Contains(argsText, "-protocol_whitelist") {
		t.Fatalf("did not expect protocol whitelist for local file, got %q", argsText)
	}
}
