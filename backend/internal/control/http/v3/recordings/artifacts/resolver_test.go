package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
)

func TestArtifactResolver_ResolveSegment(t *testing.T) {
	// Setup Dependencies
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}
	mgr, err := vod.NewManager(&dummyRunner{}, &dummyProber{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	r := New(cfg, mgr, nil)

	// Valid Service Ref (Base64 Encoded "1:0:...")
	// "1:0:1:0:0:0:0:0:0:0:/foo.ts" -> MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz
	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"
	// Expected Cache Path
	// SHA256 of ref... we assume v3recordings works.
	// But we need to know where to put the file to test "OK".

	// Since we can't easily predict the SHA hash without importing v3recordings implementation details
	// (which we shouldn't replicate), let's rely on internal helpers or integration tests?
	// OR we assume v3recordings is correct and just use it to setup.
	// We need to import v3recordings in test.

	t.Run("Segment Traversal Blocked", func(t *testing.T) {
		res, err := r.ResolveSegment(context.Background(), validID, "../secret.txt", "")
		assert.Error(t, err)
		// IsAllowedVideoSegment likely catches it first -> CodeNotFound
		// If it passed that, path confinement would catch it -> CodeInvalid
		if err != nil {
			assert.True(t, err.Code == CodeInvalid || err.Code == CodeNotFound)
		}
		assert.Empty(t, res.AbsPath)
	})

	t.Run("Forbidden Segment Type", func(t *testing.T) {
		res, err := r.ResolveSegment(context.Background(), validID, "hack.exe", "")
		assert.Error(t, err)
		if err != nil {
			assert.Equal(t, CodeNotFound, err.Code) // or Forbidden
		}
		assert.Empty(t, res.AbsPath)
	})

	// We can test success path if we can create the file in the right place.
	// But v3recordings.RecordingCacheDir is used.
	// Let's defer full integration test to the handler level or trust v3recordings.
}

type dummyRunner struct{}

func (r *dummyRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	return &dummyHandle{}, nil
}

type captureRunner struct {
	mu       sync.Mutex
	lastSpec vod.Spec
}

func (r *captureRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	r.mu.Lock()
	r.lastSpec = spec
	r.mu.Unlock()
	return &dummyHandle{}, nil
}

func (r *captureRunner) LastSpec() vod.Spec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastSpec
}

type dummyHandle struct{}

func (h *dummyHandle) Wait() error                          { return nil }
func (h *dummyHandle) Stop(grace, kill time.Duration) error { return nil }
func (h *dummyHandle) Progress() <-chan vod.ProgressEvent {
	return make(chan vod.ProgressEvent)
}
func (h *dummyHandle) Diagnostics() []string { return nil }

type dummyProber struct{}

func (p *dummyProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	return &vod.StreamInfo{}, nil
}

func TestArtifactResolver_ResolvePlaylist_UsesConcreteTargetProfile(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver.local",
			StreamPort: 17999,
		},
	}
	runner := &captureRunner{}
	mgr, err := vod.NewManager(runner, &dummyProber{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	r := New(cfg, mgr, nil)

	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"
	_, artifactErr := r.ResolvePlaylist(context.Background(), validID, "safari", "", nil)
	if artifactErr == nil || artifactErr.Code != CodePreparing {
		t.Fatalf("expected preparing artifact error, got %#v", artifactErr)
	}

	var spec vod.Spec
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		spec = runner.LastSpec()
		if spec.TargetProfile != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if spec.TargetProfile == nil {
		t.Fatal("expected concrete target profile in VOD spec")
	}
	if spec.TargetProfile.Video.Mode != "transcode" || spec.TargetProfile.Video.CRF != 23 || spec.TargetProfile.Video.Preset != "fast" || spec.TargetProfile.Audio.BitrateKbps != 256 {
		t.Fatalf("unexpected target profile passed to runner: %#v", spec.TargetProfile)
	}
	if spec.TargetProfile.Packaging != playbackprofile.PackagingFMP4 || spec.TargetProfile.HLS.SegmentContainer != "fmp4" {
		t.Fatalf("expected safari fallback to use fmp4 packaging, got %#v", spec.TargetProfile)
	}
}

func TestArtifactResolver_ResolvePlaylist_DerivesVariantFromTargetProfile(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver.local",
			StreamPort: 17999,
		},
	}
	runner := &captureRunner{}
	mgr, err := vod.NewManager(runner, &dummyProber{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	r := New(cfg, mgr, nil)

	ref := "1:0:1:0:0:0:0:0:0:0:/foo.ts"
	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"
	target := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
		Container: "mpegts",
		Packaging: playbackprofile.PackagingTS,
		Video: playbackprofile.VideoTarget{
			Mode: playbackprofile.MediaModeCopy,
		},
		Audio: playbackprofile.AudioTarget{
			Mode:        playbackprofile.MediaModeTranscode,
			Codec:       "aac",
			Channels:    2,
			BitrateKbps: 256,
			SampleRate:  48000,
		},
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: "mpegts",
			SegmentSeconds:   6,
		},
		HWAccel: playbackprofile.HWAccelNone,
	})

	_, artifactErr := r.ResolvePlaylist(context.Background(), validID, "", "", &target)
	if artifactErr == nil || artifactErr.Code != CodePreparing {
		t.Fatalf("expected preparing artifact error, got %#v", artifactErr)
	}

	expectedWorkDir, err := v3recordings.RecordingVariantCacheDir(cfg.HLS.Root, ref, target.Hash())
	if err != nil {
		t.Fatalf("RecordingVariantCacheDir failed: %v", err)
	}

	var spec vod.Spec
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		spec = runner.LastSpec()
		if spec.WorkDir != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if spec.WorkDir != expectedWorkDir {
		t.Fatalf("expected variant-aware workdir %q, got %#v", expectedWorkDir, spec)
	}
}

func TestArtifactResolver_ResolvePlaylist_RejectsVariantMismatch(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
		Enigma2: config.Enigma2Settings{
			BaseURL:    "http://receiver.local",
			StreamPort: 17999,
		},
	}
	mgr, err := vod.NewManager(&dummyRunner{}, &dummyProber{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	r := New(cfg, mgr, nil)

	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"
	target := playbackprofile.CanonicalizeTarget(playbackprofile.TargetPlaybackProfile{
		Container: "mpegts",
		Packaging: playbackprofile.PackagingTS,
		Audio: playbackprofile.AudioTarget{
			Mode:        playbackprofile.MediaModeTranscode,
			Codec:       "aac",
			Channels:    2,
			BitrateKbps: 256,
			SampleRate:  48000,
		},
		HLS: playbackprofile.HLSTarget{
			Enabled:          true,
			SegmentContainer: "mpegts",
		},
		HWAccel: playbackprofile.HWAccelNone,
	})

	_, artifactErr := r.ResolvePlaylist(context.Background(), validID, "", "wrong-variant", &target)
	if artifactErr == nil {
		t.Fatal("expected invalid artifact error")
	}
	if artifactErr.Code != CodeInvalid {
		t.Fatalf("expected invalid artifact error, got %#v", artifactErr)
	}
}

func getFallbackMetricValue() float64 {
	mfs, _ := prometheus.DefaultGatherer.Gather()
	for _, mf := range mfs {
		if mf.GetName() == "xg2g_recordings_target_fallback_total" {
			if len(mf.GetMetric()) > 0 {
				return mf.GetMetric()[0].GetCounter().GetValue()
			}
		}
	}
	return 0
}

func TestArtifactResolver_StrictTargetEnforcement_Fails(t *testing.T) {
	cfg := &config.AppConfig{
		RecordingStrictTargetRequired: true,
	}
	mgr, _ := vod.NewManager(&dummyRunner{}, &dummyProber{}, nil)
	r := New(cfg, mgr, nil)

	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"
	_, err := r.ResolvePlaylist(context.Background(), validID, "safari", "", nil)

	assert.NotNil(t, err)
	assert.Equal(t, CodeInvalid, err.Code)
	assert.Contains(t, err.Detail, "handshake required")
	assert.Empty(t, mgr.ActiveJobIDs(), "no build job should be triggered when target is missing in strict mode")
}

func TestArtifactResolver_LegacyFallback_Metrics(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{Root: t.TempDir()},
		RecordingStrictTargetRequired: false,
	}
	runner := &captureRunner{}
	mgr, _ := vod.NewManager(runner, &dummyProber{}, nil)
	r := New(cfg, mgr, nil)

	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"

	before := getFallbackMetricValue()

	_, err := r.ResolvePlaylist(context.Background(), validID, "safari", "", nil)
	assert.NotNil(t, err)
	assert.Equal(t, CodePreparing, err.Code)

	after := getFallbackMetricValue()
	assert.Equal(t, float64(1), after-before, "fallback metric should increment by 1")
}

func TestArtifactResolver_ResolvePlaylistState(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{Root: t.TempDir()},
	}
	mgr, _ := vod.NewManager(&dummyRunner{}, &dummyProber{}, nil)
	r := New(cfg, mgr, nil)

	ref := "1:0:1:0:0:0:0:0:0:0:/foo.ts"
	validID := "MTowOjE6MDowOjA6MDowOjA6MDovZm9vLnRz"
	variant := "v1"

	t.Run("Missing Playlist -> CodeNotFound", func(t *testing.T) {
		_, err := r.ResolvePlaylistState(context.Background(), validID, variant)
		assert.NotNil(t, err)
		assert.Equal(t, CodeNotFound, err.Code)
		assert.Empty(t, mgr.ActiveJobIDs(), "state query should not trigger a build job")
	})

	t.Run("Existing Playlist -> Returns Content", func(t *testing.T) {
		dir, _ := v3recordings.RecordingVariantCacheDir(cfg.HLS.Root, ref, variant)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U"), 0644)

		res, err := r.ResolvePlaylistState(context.Background(), validID, variant)
		assert.Nil(t, err)
		assert.Equal(t, ArtifactKindPlaylist, res.Kind)
		assert.Equal(t, []byte("#EXTM3U\n#EXT-X-PLAYLIST-TYPE:EVENT"), res.Data)
		assert.Empty(t, mgr.ActiveJobIDs(), "state query should not trigger a build job")
	})

	t.Run("Empty Variant -> Resolves to Default Copy Profile", func(t *testing.T) {
		// Construct the expected default hash
		target := recordingTargetProfile("")
		expectedVariant := target.Hash()

		dir, _ := v3recordings.RecordingVariantCacheDir(cfg.HLS.Root, ref, expectedVariant)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "index.m3u8"), []byte("#EXTM3U"), 0644)

		// Call with empty variant
		res, err := r.ResolvePlaylistState(context.Background(), validID, "")
		assert.Nil(t, err)
		assert.Equal(t, ArtifactKindPlaylist, res.Kind)
		assert.Equal(t, []byte("#EXTM3U\n#EXT-X-PLAYLIST-TYPE:EVENT"), res.Data)
	})
}
