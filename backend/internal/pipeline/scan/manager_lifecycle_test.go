package scan

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lifecycleTestStore struct {
	closed atomic.Int32
}

func (s *lifecycleTestStore) Update(cap Capability)                    {}
func (s *lifecycleTestStore) Get(serviceRef string) (Capability, bool) { return Capability{}, false }
func (s *lifecycleTestStore) Close() error {
	s.closed.Add(1)
	return nil
}

func TestManager_StopCancelsBackgroundAndJoins(t *testing.T) {
	store := &lifecycleTestStore{}
	manager := NewManager(store, t.TempDir()+"/playlist.m3u", nil)

	started := make(chan struct{})
	manager.scanFn = func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()
	manager.AttachLifecycle(parentCtx)

	require.True(t, manager.RunBackground())
	<-started

	done := make(chan struct{})
	go func() {
		manager.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not join background scan")
	}

	assert.False(t, manager.isScanning.Load())
}

func TestManager_AttachedParentCancelStopsBackground(t *testing.T) {
	store := &lifecycleTestStore{}
	manager := NewManager(store, t.TempDir()+"/playlist.m3u", nil)

	stopped := make(chan struct{})
	manager.scanFn = func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return ctx.Err()
	}

	parentCtx, parentCancel := context.WithCancel(context.Background())
	manager.AttachLifecycle(parentCtx)
	require.True(t, manager.RunBackground())

	parentCancel()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("background scan did not stop when parent context was canceled")
	}

	manager.Stop()
	assert.False(t, manager.isScanning.Load())
}

func TestManager_CloseStopsBackgroundBeforeStoreClose(t *testing.T) {
	store := &lifecycleTestStore{}
	manager := NewManager(store, t.TempDir()+"/playlist.m3u", nil)

	started := make(chan struct{})
	manager.scanFn = func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	require.True(t, manager.RunBackground())
	<-started

	require.NoError(t, manager.Close())
	assert.Equal(t, int32(1), store.closed.Load())
	assert.False(t, manager.isScanning.Load())
}
