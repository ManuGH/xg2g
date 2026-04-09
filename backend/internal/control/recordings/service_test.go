package recordings

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/vod"
	internalrecordings "github.com/ManuGH/xg2g/internal/recordings"
)

// Unique Mocks for service_test.go
// (MockOWIClient, MockProber, MockPathMapper are already in duration_truth_test.go)

type MockResumeStore struct {
	mock.Mock
}

func (m *MockResumeStore) GetResume(ctx context.Context, principalID, serviceRef string) (ResumeData, bool, error) {
	args := m.Called(ctx, principalID, serviceRef)
	return args.Get(0).(ResumeData), args.Bool(1), args.Error(2)
}

type MockResolverForService struct {
	mock.Mock
}

func (m *MockResolverForService) Resolve(ctx context.Context, serviceRef string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	args := m.Called(ctx, serviceRef, intent, profile)
	return args.Get(0).(PlaybackInfoResult), args.Error(1)
}

func (m *MockResolverForService) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	// Simple stub for tests that don't verify truth specifically
	args := m.Called(ctx, recordingID)
	return args.Get(0).(playback.MediaTruth), args.Error(1)
}

func (m *MockResolverForService) truthProvider() *truthProvider {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*truthProvider)
}

func (m *MockResolverForService) ProbeManager() *probeManager {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(*probeManager)
}

type MockRunnerForService struct {
	mock.Mock
}

func (m *MockRunnerForService) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	args := m.Called(ctx, spec)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(vod.Handle), args.Error(1)
}

type MockHandleForService struct {
	mock.Mock
}

func (m *MockHandleForService) Wait() error { return m.Called().Error(0) }
func (m *MockHandleForService) Stop(grace, kill time.Duration) error {
	return m.Called(grace, kill).Error(0)
}
func (m *MockHandleForService) Progress() <-chan vod.ProgressEvent {
	return m.Called().Get(0).(chan vod.ProgressEvent)
}
func (m *MockHandleForService) Diagnostics() []string { return m.Called().Get(0).([]string) }

func setupServiceTest(t *testing.T) (*service, *MockOWIClient, *MockResumeStore, *MockResolverForService, *vod.Manager) {
	cfg := &config.AppConfig{
		RecordingRoots: map[string]string{
			"movies": "/media/hdd/movie",
		},
		HLS: config.HLSConfig{
			Root: "/tmp/xg2g-hls",
		},
	}
	owi := new(MockOWIClient)
	res := new(MockResumeStore)
	slv := new(MockResolverForService)

	// Lite VOD Manager
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	mapper := new(MockPathMapper)
	vm, err := vod.NewManager(runner, prober, mapper)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	s, err := NewService(cfg, vm, slv, owi, res)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	return s.(*service), owi, res, slv, vm
}

const validTestRef = "1:0:1:0:0:0:0:0:0:0:/media/hdd/movie/test.ts"

func TestService_List_Success(t *testing.T) {
	s, owi, _, _, _ := setupServiceTest(t)

	owi.On("GetLocations", mock.Anything).Return([]OWILocation{}, nil)
	owi.On("GetTimers", mock.Anything).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", mock.Anything, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{
				ServiceRef: validTestRef,
				Title:      "Title 1",
				Length:     "01:00:00",
			},
		},
	}, nil)

	result, err := s.List(context.Background(), ListInput{RootID: "movies"})
	assert.NoError(t, err)
	assert.Len(t, result.Recordings, 1)
	assert.Equal(t, "Title 1", result.Recordings[0].Title)
	assert.Equal(t, EncodeRecordingID(validTestRef), result.Recordings[0].RecordingID)
}

func TestService_List_UsesReceiverLocationAsDefaultRoot(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: "/tmp/xg2g-hls",
		},
	}
	owi := new(MockOWIClient)
	resumeStore := new(MockResumeStore)
	resolver := new(MockResolverForService)
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	mapper := new(MockPathMapper)

	vm, err := vod.NewManager(runner, prober, mapper)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	svc, err := NewService(cfg, vm, resolver, owi, resumeStore)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	const receiverRoot = "/media/nfs-recordings"
	const receiverRef = "1:0:0:0:0:0:0:0:0:0:/media/nfs-recordings/Monk.ts"

	owi.On("GetLocations", mock.Anything).Return([]OWILocation{
		{Name: "nfs-recordings", Path: receiverRoot},
	}, nil)
	owi.On("GetTimers", mock.Anything).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", mock.Anything, receiverRoot).Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{
				ServiceRef: receiverRef,
				Title:      "Monk",
				Length:     "40:25",
			},
		},
	}, nil)

	result, err := svc.List(context.Background(), ListInput{})
	assert.NoError(t, err)
	assert.Equal(t, "nfs-recordings", result.CurrentRoot)
	assert.Len(t, result.Roots, 1)
	assert.Equal(t, "nfs-recordings", result.Roots[0].ID)
	assert.Len(t, result.Recordings, 1)
	assert.Equal(t, "Monk", result.Recordings[0].Title)
}

func TestService_List_FallsBackToLegacyHddWhenNoRootsDiscovered(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: "/tmp/xg2g-hls",
		},
	}
	owi := new(MockOWIClient)
	resumeStore := new(MockResumeStore)
	resolver := new(MockResolverForService)
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	mapper := new(MockPathMapper)

	vm, err := vod.NewManager(runner, prober, mapper)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	svc, err := NewService(cfg, vm, resolver, owi, resumeStore)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	owi.On("GetLocations", mock.Anything).Return([]OWILocation{}, nil)
	owi.On("GetTimers", mock.Anything).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", mock.Anything, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{},
	}, nil)

	result, err := svc.List(context.Background(), ListInput{})
	assert.NoError(t, err)
	assert.Equal(t, "hdd", result.CurrentRoot)
	assert.Len(t, result.Roots, 1)
	assert.Equal(t, "hdd", result.Roots[0].ID)
}

func TestService_ResolvePlayback_Success(t *testing.T) {
	s, _, _, slv, _ := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	slv.On("Resolve", mock.Anything, validTestRef, PlaybackIntent("stream"), PlaybackProfile("hd")).Return(PlaybackInfoResult{
		Decision: playback.Decision{
			Artifact: playback.ArtifactHLS,
		},
		MediaInfo: playback.MediaInfo{
			Duration: 3600,
		},
		Reason: "test",
	}, nil)

	result, err := s.ResolvePlayback(context.Background(), hexID, "hd")
	assert.NoError(t, err)
	assert.Equal(t, StrategyHLS, result.Strategy)
	assert.True(t, result.CanSeek)
}

func TestService_GetStatus_Ready(t *testing.T) {
	s, _, _, _, vm := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	vm.SeedMetadata(RecordingVariantMetadataKey(validTestRef, DefaultRecordingVariantHash()), vod.Metadata{
		State: vod.ArtifactStateReady,
	})

	result, err := s.GetStatus(context.Background(), StatusInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.Equal(t, "READY", result.State)
}

func TestService_GetStatus_RehydratesDefaultPlaylistFromDisk(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}
	owi := new(MockOWIClient)
	resumeStore := new(MockResumeStore)
	resolver := new(MockResolverForService)
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	vm, err := vod.NewManager(runner, prober, new(MockPathMapper))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	svc, err := NewService(cfg, vm, resolver, owi, resumeStore)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	recordingID := EncodeRecordingID(validTestRef)
	defaultVariant := DefaultRecordingVariantHash()
	cacheDir, err := RecordingVariantCacheDir(cfg.HLS.Root, validTestRef, defaultVariant)
	assert.NoError(t, err)
	assert.NoError(t, os.MkdirAll(cacheDir, 0750))

	playlistPath := filepath.Join(cacheDir, "index.m3u8")
	assert.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), 0600))

	result, err := svc.GetStatus(context.Background(), StatusInput{RecordingID: recordingID})
	assert.NoError(t, err)
	assert.Equal(t, "READY", result.State)

	meta, ok := vm.GetMetadata(RecordingVariantMetadataKey(validTestRef, defaultVariant))
	assert.True(t, ok)
	assert.Equal(t, vod.ArtifactStateReady, meta.State)
	assert.Equal(t, playlistPath, meta.PlaylistPath)
}

func TestService_Stream_NotReady_TriggersProbe(t *testing.T) {
	s, _, _, _, vm := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	// Metadata doesn't exist yet

	result, err := s.Stream(context.Background(), StreamInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.False(t, result.Ready)
	assert.Equal(t, "UNKNOWN", result.State)

	// Verify probe triggered (using Eventually for async)
	assert.Eventually(t, func() bool {
		meta, ok := vm.GetMetadata(validTestRef)
		return ok && meta.State == vod.ArtifactStatePreparing
	}, 1*time.Second, 10*time.Millisecond)
}

func TestService_Stream_ReadyLocalTsUsesResolvedPath(t *testing.T) {
	s, _, _, _, vm := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)
	tmpDir := t.TempDir()
	localPath := filepath.Join(tmpDir, "Monk.ts")
	assert.NoError(t, os.WriteFile(localPath, []byte("ts-data"), 0644))

	vm.SeedMetadata(validTestRef, vod.Metadata{
		State:        vod.ArtifactStateReady,
		ResolvedPath: localPath,
		Container:    "mpegts",
		VideoCodec:   "h264",
		AudioCodec:   "ac3",
	})

	result, err := s.Stream(context.Background(), StreamInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.True(t, result.Ready)
	assert.Equal(t, localPath, result.LocalPath)
	assert.Equal(t, "video/mp2t", result.ContentType)
	assert.Equal(t, CacheNoStore, result.CachePolicy)
}

func TestService_Stream_RehydratesLocalSourceWithoutMetadata(t *testing.T) {
	localRoot := t.TempDir()
	localPath := filepath.Join(localRoot, "test.ts")
	assert.NoError(t, os.WriteFile(localPath, []byte("ts-data"), 0644))
	resolvedPath, err := filepath.EvalSymlinks(localPath)
	assert.NoError(t, err)

	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
		RecordingPathMappings: []config.RecordingPathMapping{
			{ReceiverRoot: "/media/hdd/movie", LocalRoot: localRoot},
		},
	}
	owi := new(MockOWIClient)
	resumeStore := new(MockResumeStore)
	resolver := new(MockResolverForService)
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	mapper := internalrecordings.NewPathMapper(cfg.RecordingPathMappings)
	vm, err := vod.NewManager(runner, prober, mapper)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	svc, err := NewService(cfg, vm, resolver, owi, resumeStore)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	result, err := svc.Stream(context.Background(), StreamInput{RecordingID: EncodeRecordingID(validTestRef)})
	assert.NoError(t, err)
	assert.True(t, result.Ready)
	assert.Equal(t, resolvedPath, result.LocalPath)
	assert.Equal(t, "video/mp2t", result.ContentType)
	assert.Equal(t, CacheNoStore, result.CachePolicy)

	meta, ok := vm.GetMetadata(validTestRef)
	assert.True(t, ok)
	assert.Equal(t, vod.ArtifactStateReady, meta.State)
	assert.Equal(t, resolvedPath, meta.ResolvedPath)
}

func TestService_Stream_DoesNotTreatHLSPlaylistAsDirectStreamTruth(t *testing.T) {
	cfg := &config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}
	owi := new(MockOWIClient)
	resumeStore := new(MockResumeStore)
	resolver := new(MockResolverForService)
	runner := new(MockRunnerForService)
	prober := new(MockProber)
	vm, err := vod.NewManager(runner, prober, new(MockPathMapper))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	svc, err := NewService(cfg, vm, resolver, owi, resumeStore)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	cacheDir, err := RecordingVariantCacheDir(cfg.HLS.Root, validTestRef, DefaultRecordingVariantHash())
	assert.NoError(t, err)
	assert.NoError(t, os.MkdirAll(cacheDir, 0750))
	assert.NoError(t, os.WriteFile(filepath.Join(cacheDir, "index.m3u8"), []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), 0600))

	result, err := svc.Stream(context.Background(), StreamInput{RecordingID: EncodeRecordingID(validTestRef)})
	assert.NoError(t, err)
	assert.False(t, result.Ready)
	assert.Equal(t, "UNKNOWN", result.State)

	_, variantMetaOk := vm.GetMetadata(RecordingVariantMetadataKey(validTestRef, DefaultRecordingVariantHash()))
	assert.False(t, variantMetaOk, "direct stream must not silently adopt HLS-ready metadata")
}

func TestService_Delete_Success(t *testing.T) {
	s, owi, _, _, _ := setupServiceTest(t)

	hexID := EncodeRecordingID(validTestRef)

	owi.On("DeleteRecording", mock.Anything, validTestRef).Return(nil)

	result, err := s.Delete(context.Background(), DeleteInput{RecordingID: hexID})
	assert.NoError(t, err)
	assert.True(t, result.Deleted)
	owi.AssertExpectations(t)
}
