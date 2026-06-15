package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/stretchr/testify/require"
)

// M10: when the CALLER's context is canceled mid-probe (e.g. client disconnect), the probe
// was aborted, not failed. ProbeCapability must not persist a failure record (which would
// impose a 24h retry lockout for a transient cancellation).
func TestManager_ProbeCapability_CanceledCallerDoesNotPersistFailure(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "1:0:1:ABC"
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // caller aborts

	_, _, err := manager.ProbeCapability(ctx, serviceRef)
	require.Error(t, err)

	if _, storedFound := store.Get(serviceRef); storedFound {
		t.Fatal("a canceled caller must not persist a capability failure record (24h lockout)")
	}
}
