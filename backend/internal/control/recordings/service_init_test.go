package recordings

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockMinimalResolver struct {
	mock.Mock
}

func (m *mockMinimalResolver) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	return PlaybackInfoResult{}, nil
}

// Does NOT implement provider interface
func (m *mockMinimalResolver) truthProvider() *truthProvider { return nil }
func (m *mockMinimalResolver) ProbeManager() *probeManager   { return nil }
func (m *mockMinimalResolver) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, nil
}

type mockFullResolver struct {
	mock.Mock
}

func (m *mockFullResolver) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	return PlaybackInfoResult{}, nil
}
func (m *mockFullResolver) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	return playback.MediaTruth{}, nil
}
func (m *mockFullResolver) truthProvider() *truthProvider {
	args := m.Called()
	return args.Get(0).(*truthProvider)
}
func (m *mockFullResolver) ProbeManager() *probeManager {
	args := m.Called()
	return args.Get(0).(*probeManager)
}

func TestNewService_DI_Hardening(t *testing.T) {
	cfg := &config.AppConfig{}
	mgr := &vod.Manager{}
	owi := (OWIClient)(nil)
	resume := (ResumeStore)(nil)

	t.Run("Fail when resolver is nil", func(t *testing.T) {
		s, err := NewService(cfg, mgr, nil, owi, resume)
		assert.Error(t, err)
		assert.Nil(t, s)
	})

	t.Run("Success with valid dependencies", func(t *testing.T) {
		res := new(mockMinimalResolver)
		s, err := NewService(cfg, mgr, res, owi, resume)
		assert.NoError(t, err)
		assert.NotNil(t, s)
	})
}
