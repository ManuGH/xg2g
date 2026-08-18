package vod

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
	"github.com/stretchr/testify/require"
)

func TestManagerShutdownContext_DrainsProberWorkers(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()
	mgr.StartProberPool(rootCtx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, mgr.ShutdownContext(shutdownCtx))
}

func TestManagerShutdownContext_DrainsBuildWorkers(t *testing.T) {
	progress := make(chan ProgressEvent)
	runner := NewMockRunner(nil, &MockHandleBehavior{
		WaitBlocks:   true,
		StopUnblocks: true,
		ProgressChan: progress,
	})
	mgr, err := NewManager(runner, &mockProber{}, nil)
	require.NoError(t, err)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()
	mgr.StartProberPool(rootCtx)

	workDir := t.TempDir()
	outputTemp := "index.live.m3u8"
	require.NoError(t, os.WriteFile(filepath.Join(workDir, outputTemp), []byte("#EXTM3U"), 0600))

	dummyTarget := &ports.BuildIntent{
		Target: ports.TargetPlaybackProfile{
			Video: ports.VideoTarget{Mode: ports.MediaModeCopy},
		},
	}
	_, err = mgr.StartBuild(context.Background(), "job-1", "meta-1", "/tmp/input.ts", workDir, outputTemp, "", dummyTarget)
	require.NoError(t, err)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, mgr.ShutdownContext(shutdownCtx))
}

func TestManagerStartProberPool_RebindsCanceledContext(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	ctx1, cancel1 := context.WithCancel(context.Background())
	mgr.StartProberPool(ctx1)
	require.True(t, mgr.started)

	// Cancel first context (e.g. server restart/root context replaced)
	cancel1()
	require.Error(t, mgr.ctx.Err())

	// Re-bind with fresh context
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	mgr.StartProberPool(ctx2)
	require.NoError(t, mgr.ctx.Err())
}
