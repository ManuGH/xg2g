package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
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

func TestPlaylistReadyTimeout_UsesExtendedTimeoutForRecoveryProfiles(t *testing.T) {
	orch := &Orchestrator{}

	timeout := orch.playlistReadyTimeout(model.ProfileSpec{Name: profiles.ProfileRepair}, false)
	if timeout != defaultRecoveryPlaylistReadyTimeout {
		t.Fatalf("expected repair profile timeout %v, got %v", defaultRecoveryPlaylistReadyTimeout, timeout)
	}

	timeout = orch.playlistReadyTimeout(model.ProfileSpec{Name: profiles.ProfileSafariDirty}, false)
	if timeout != defaultRecoveryPlaylistReadyTimeout {
		t.Fatalf("expected safari_dirty timeout %v, got %v", defaultRecoveryPlaylistReadyTimeout, timeout)
	}
}

func TestPlaylistReadyTimeout_ExtendsSafariCPUTranscodeStartup(t *testing.T) {
	orch := &Orchestrator{}

	timeout := orch.playlistReadyTimeout(model.ProfileSpec{
		Name:           profiles.ProfileSafari,
		TranscodeVideo: true,
	}, false)
	if timeout != defaultSafariCPUPlaylistReadyTimeout {
		t.Fatalf("expected safari cpu timeout %v, got %v", defaultSafariCPUPlaylistReadyTimeout, timeout)
	}

	timeout = orch.playlistReadyTimeout(model.ProfileSpec{
		Name:           profiles.ProfileSafari,
		TranscodeVideo: true,
		HWAccel:        "vaapi",
	}, false)
	if timeout != defaultSafariPlaylistReadyTimeout {
		t.Fatalf("expected safari gpu timeout %v, got %v", defaultSafariPlaylistReadyTimeout, timeout)
	}
}

func TestPlaylistReadyTimeout_ExtendsSafariHQ50Startup(t *testing.T) {
	orch := &Orchestrator{}

	timeout := orch.playlistReadyTimeout(model.ProfileSpec{
		Name:                 profiles.ProfileSafari,
		TranscodeVideo:       true,
		EffectiveRuntimeMode: ports.RuntimeModeHQ50,
	}, false)
	if timeout != defaultSafariHQ50PlaylistReadyTimeout {
		t.Fatalf("expected safari hq50 timeout %v, got %v", defaultSafariHQ50PlaylistReadyTimeout, timeout)
	}
}
