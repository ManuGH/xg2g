package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trackingStateStore struct {
	sessionstore.StateStore
	closed atomic.Int32
}

func (s *trackingStateStore) Close() error {
	s.closed.Add(1)
	return nil
}

type trackingResumeStore struct {
	resume.Store
	closed atomic.Int32
}

func (s *trackingResumeStore) Close() error {
	s.closed.Add(1)
	return nil
}

type trackingScanStore struct {
	closed atomic.Int32
}

func (s *trackingScanStore) Close() error {
	s.closed.Add(1)
	return nil
}

func TestManager_Start_ShutdownClosesRuntimeStoresWhenEngineDisabled(t *testing.T) {
	v3Store := &trackingStateStore{StateStore: sessionstore.NewMemoryStore()}
	resumeStore := &trackingResumeStore{Store: resume.NewMemoryStore()}
	scanStore := &trackingScanStore{}

	mgrIface, err := NewManager(config.ServerConfig{ShutdownTimeout: 2 * time.Second}, Deps{
		Logger:      log.WithComponent("test"),
		Config:      config.AppConfig{Engine: config.EngineConfig{Enabled: false}},
		ProxyOnly:   true,
		V3Store:     v3Store,
		ResumeStore: resumeStore,
		ScanManager: scanStore,
	})
	require.NoError(t, err)

	mgr := mgrIface.(*manager)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Start(ctx)
	}()

	cancel()

	select {
	case startErr := <-errCh:
		require.NoError(t, startErr)
	case <-time.After(3 * time.Second):
		t.Fatal("manager.Start did not return after cancellation")
	}

	assert.Equal(t, int32(1), v3Store.closed.Load())
	assert.Equal(t, int32(1), resumeStore.closed.Load())
	assert.Equal(t, int32(1), scanStore.closed.Load())
}
