package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

func TestCheckPlaylistReadyAt_LiveRequiresConfiguredSegmentCount(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")
	if err := os.WriteFile(playlistPath, []byte(`#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:6.000000,
seg_000000.ts
#EXTINF:6.000000,
seg_000001.ts
`), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}
	for _, name := range []string{"seg_000000.ts", "seg_000001.ts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("segment"), 0o600); err != nil {
			t.Fatalf("write segment %s: %v", name, err)
		}
	}

	orch := &Orchestrator{LiveReadySegments: 3}
	ttfpRecorded := false

	ready, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "high", time.Now())
	if err != nil {
		t.Fatalf("checkPlaylistReadyAt() error = %v", err)
	}
	if ready {
		t.Fatal("expected live playlist with only 2 segments to stay not ready")
	}
}

func TestCheckPlaylistReadyAt_LiveReadyAfterRequiredSegmentsExist(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")
	if err := os.WriteFile(playlistPath, []byte(`#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:6.000000,
seg_000000.ts
#EXTINF:6.000000,
seg_000001.ts
#EXTINF:6.000000,
seg_000002.ts
`), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}
	for _, name := range []string{"seg_000000.ts", "seg_000001.ts", "seg_000002.ts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("segment"), 0o600); err != nil {
			t.Fatalf("write segment %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, model.SessionFirstFrameMarkerFilename), []byte("ready"), 0o600); err != nil {
		t.Fatalf("write first-frame marker: %v", err)
	}

	orch := &Orchestrator{LiveReadySegments: 3}
	ttfpRecorded := false

	ready, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "high", time.Now())
	if err != nil {
		t.Fatalf("checkPlaylistReadyAt() error = %v", err)
	}
	if !ready {
		t.Fatal("expected live playlist with 3 segments to be ready")
	}
}

func TestCheckPlaylistReadyAt_LiveRequiresFirstFrameMarker(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")
	if err := os.WriteFile(playlistPath, []byte(`#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:6.000000,
seg_000000.ts
#EXTINF:6.000000,
seg_000001.ts
#EXTINF:6.000000,
seg_000002.ts
`), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}
	for _, name := range []string{"seg_000000.ts", "seg_000001.ts", "seg_000002.ts"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("segment"), 0o600); err != nil {
			t.Fatalf("write segment %s: %v", name, err)
		}
	}

	orch := &Orchestrator{LiveReadySegments: 3}
	ttfpRecorded := false

	ready, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "high", time.Now())
	if err != nil {
		t.Fatalf("checkPlaylistReadyAt() error = %v", err)
	}
	if ready {
		t.Fatal("expected live playlist without first-frame marker to stay not ready")
	}
}

func TestCheckPlaylistReadyAt_LiveFMP4RequiresInitSegment(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")
	if err := os.WriteFile(playlistPath, []byte(`#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:6
#EXT-X-MAP:URI="init.mp4"
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:6.000000,
seg_000000.m4s
#EXTINF:6.000000,
seg_000001.m4s
#EXTINF:6.000000,
seg_000002.m4s
`), 0o600); err != nil {
		t.Fatalf("write playlist: %v", err)
	}
	for _, name := range []string{"seg_000000.m4s", "seg_000001.m4s", "seg_000002.m4s"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("segment"), 0o600); err != nil {
			t.Fatalf("write segment %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, model.SessionFirstFrameMarkerFilename), []byte("ready"), 0o600); err != nil {
		t.Fatalf("write first-frame marker: %v", err)
	}

	orch := &Orchestrator{LiveReadySegments: 3}
	ttfpRecorded := false

	ready, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "high", time.Now())
	if err != nil {
		t.Fatalf("checkPlaylistReadyAt() error = %v", err)
	}
	if ready {
		t.Fatal("expected fMP4 live playlist without init segment to stay not ready")
	}
}
