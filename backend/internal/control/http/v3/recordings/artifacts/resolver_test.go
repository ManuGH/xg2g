package artifacts

import (
	"context"
	"sync"
	"testing"
	"time"

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
	if spec.TargetProfile.Video.Mode != "transcode" || spec.TargetProfile.Audio.BitrateKbps != 128 {
		t.Fatalf("unexpected target profile passed to runner: %#v", spec.TargetProfile)
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
