package paths

import "testing"

func TestHLSLayoutRoots(t *testing.T) {
	hlsRoot := "/var/lib/xg2g/hls"

	if got := LiveSessionsRoot(hlsRoot); got != "/var/lib/xg2g/hls/sessions" {
		t.Fatalf("LiveSessionsRoot() = %q", got)
	}
	if got := RecordingArtifactsRoot(hlsRoot); got != "/var/lib/xg2g/hls/recordings" {
		t.Fatalf("RecordingArtifactsRoot() = %q", got)
	}
}

func TestHLSLayoutArtifactPaths(t *testing.T) {
	hlsRoot := "/var/lib/xg2g/hls"

	if got := LiveSessionDir(hlsRoot, "session-1"); got != "/var/lib/xg2g/hls/sessions/session-1" {
		t.Fatalf("LiveSessionDir() = %q", got)
	}
	if got := LiveSessionArtifactRelPath("session-1", "index.m3u8"); got != "sessions/session-1/index.m3u8" {
		t.Fatalf("LiveSessionArtifactRelPath() = %q", got)
	}
	if got := RecordingArtifactDir(hlsRoot, "abc123"); got != "/var/lib/xg2g/hls/recordings/abc123" {
		t.Fatalf("RecordingArtifactDir() = %q", got)
	}
}
