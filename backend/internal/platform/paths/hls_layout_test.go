package paths

import (
	"path/filepath"
	"testing"
)

func TestHLSLayoutRoots(t *testing.T) {
	hlsRoot := "/var/lib/xg2g/hls"
	expectedLive := LiveHLSRoot(hlsRoot)

	if got := LiveSessionsRoot(hlsRoot); got != filepath.Join(expectedLive, "sessions") {
		t.Fatalf("LiveSessionsRoot() = %q", got)
	}
	if got := RecordingArtifactsRoot(hlsRoot); got != "/var/lib/xg2g/hls/recordings" {
		t.Fatalf("RecordingArtifactsRoot() = %q", got)
	}
}

func TestHLSLayoutArtifactPaths(t *testing.T) {
	hlsRoot := "/var/lib/xg2g/hls"
	expectedLive := LiveHLSRoot(hlsRoot)

	if got := LiveSessionDir(hlsRoot, "session-1"); got != filepath.Join(expectedLive, "sessions", "session-1") {
		t.Fatalf("LiveSessionDir() = %q", got)
	}
	if got := LiveSessionArtifactRelPath("session-1", "index.m3u8"); got != "sessions/session-1/index.m3u8" {
		t.Fatalf("LiveSessionArtifactRelPath() = %q", got)
	}
	if got := RecordingArtifactDir(hlsRoot, "abc123"); got != "/var/lib/xg2g/hls/recordings/abc123" {
		t.Fatalf("RecordingArtifactDir() = %q", got)
	}
}

func TestLiveHLSRootForPolicy(t *testing.T) {
	hlsRoot := "/var/lib/xg2g/hls"

	// When DVRWindowSec > 300, should always use persistent disk hlsRoot
	if got := LiveHLSRootForPolicy(hlsRoot, 3600); got != hlsRoot {
		t.Fatalf("LiveHLSRootForPolicy(3600) = %q, want %q", got, hlsRoot)
	}
	if got := LiveHLSRootForPolicy(hlsRoot, 600); got != hlsRoot {
		t.Fatalf("LiveHLSRootForPolicy(600) = %q, want %q", got, hlsRoot)
	}
}
