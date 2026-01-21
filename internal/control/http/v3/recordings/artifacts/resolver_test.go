package artifacts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
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
		res, err := r.ResolveSegment(context.Background(), validID, "../secret.txt")
		assert.Error(t, err)
		// IsAllowedVideoSegment likely catches it first -> CodeNotFound
		// If it passed that, path confinement would catch it -> CodeInvalid
		if err != nil {
			assert.True(t, err.Code == CodeInvalid || err.Code == CodeNotFound)
		}
		assert.Empty(t, res.AbsPath)
	})

	t.Run("Forbidden Segment Type", func(t *testing.T) {
		res, err := r.ResolveSegment(context.Background(), validID, "hack.exe")
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
