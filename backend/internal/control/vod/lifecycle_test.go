package vod

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	_, err = mgr.StartBuild(context.Background(), "job-1", "meta-1", "/tmp/input.ts", workDir, outputTemp, "", ProfileDefault)
	require.NoError(t, err)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, mgr.ShutdownContext(shutdownCtx))
}
