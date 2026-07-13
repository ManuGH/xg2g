package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachedRefreshStatus_RestoresPlayableChannelCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlist.m3u8")
	content := "#EXTM3U\n#EXTINF:-1,One\nhttp://one\n#EXTINF:-1,Two\nhttp://two\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}

	status, ok := cachedRefreshStatus(path, "test-version")
	if !ok {
		t.Fatal("expected cached playlist status")
	}
	if status.Channels != 2 {
		t.Fatalf("Channels = %d, want 2", status.Channels)
	}
	if status.LastRun.IsZero() {
		t.Fatal("LastRun must use the playlist modification time")
	}
	if status.Version != "test-version" {
		t.Fatalf("Version = %q, want test-version", status.Version)
	}
}

func TestCachedRefreshStatus_RejectsInvalidOrEmptyPlaylist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlist.m3u8")
	if err := os.WriteFile(path, []byte("#EXTM3U\n"), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}
	if _, ok := cachedRefreshStatus(path, "test"); ok {
		t.Fatal("empty playlist must not satisfy readiness")
	}
}
