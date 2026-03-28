// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package config

import "testing"

func TestDecisionSecretFromLookup(t *testing.T) {
	t.Run("trimmed value", func(t *testing.T) {
		got := DecisionSecretFromLookup(func(key string) (string, bool) {
			if key != "XG2G_DECISION_SECRET" {
				t.Fatalf("unexpected key lookup: %s", key)
			}
			return "  top-secret-value  ", true
		})
		if string(got) != "top-secret-value" {
			t.Fatalf("DecisionSecretFromLookup() = %q, want %q", string(got), "top-secret-value")
		}
	})

	t.Run("unset returns nil", func(t *testing.T) {
		if got := DecisionSecretFromLookup(func(string) (string, bool) { return "", false }); got != nil {
			t.Fatalf("DecisionSecretFromLookup() = %q, want nil", string(got))
		}
	})

	t.Run("whitespace returns nil", func(t *testing.T) {
		if got := DecisionSecretFromLookup(func(string) (string, bool) { return "   ", true }); got != nil {
			t.Fatalf("DecisionSecretFromLookup() = %q, want nil", string(got))
		}
	})
}

func TestReadOSRuntimeEnvUsesCentralProcessEnvSource(t *testing.T) {
	prevGet := processGetEnv
	processGetEnv = func(key string) string {
		switch key {
		case "XG2G_PLAYLIST_FILENAME":
			return "runtime-playlist.m3u8"
		case "XG2G_USE_PROXY_URLS":
			return "true"
		case "XG2G_HLS_OUTPUT_DIR":
			return "/tmp/runtime-hls"
		default:
			return ""
		}
	}
	t.Cleanup(func() { processGetEnv = prevGet })

	env, err := ReadOSRuntimeEnv()
	if err != nil {
		t.Fatalf("ReadOSRuntimeEnv() error = %v", err)
	}
	if env.Runtime.PlaylistFilename != "runtime-playlist.m3u8" {
		t.Fatalf("ReadOSRuntimeEnv() playlist = %q, want %q", env.Runtime.PlaylistFilename, "runtime-playlist.m3u8")
	}
	if !env.Runtime.UseProxyURLs {
		t.Fatal("ReadOSRuntimeEnv() UseProxyURLs = false, want true")
	}
	if env.Runtime.HLS.OutputDir != "/tmp/runtime-hls" {
		t.Fatalf("ReadOSRuntimeEnv() HLS.OutputDir = %q, want %q", env.Runtime.HLS.OutputDir, "/tmp/runtime-hls")
	}
	if got := expandEnv("${XG2G_HLS_OUTPUT_DIR}/segments"); got != "/tmp/runtime-hls/segments" {
		t.Fatalf("expandEnv() = %q, want %q", got, "/tmp/runtime-hls/segments")
	}
}
