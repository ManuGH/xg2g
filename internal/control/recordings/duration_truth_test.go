package recordings

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks

type MockOWIClient struct {
	mock.Mock
}

func (m *MockOWIClient) GetLocations(ctx context.Context) ([]OWILocation, error) {
	args := m.Called(ctx)
	return args.Get(0).([]OWILocation), args.Error(1)
}

func (m *MockOWIClient) GetRecordings(ctx context.Context, path string) (OWIRecordingsList, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(OWIRecordingsList), args.Error(1)
}

func (m *MockOWIClient) DeleteRecording(ctx context.Context, serviceRef string) error {
	return m.Called(ctx, serviceRef).Error(0)
}

func (m *MockOWIClient) GetTimers(ctx context.Context) ([]OWITimer, error) {
	args := m.Called(ctx)
	return args.Get(0).([]OWITimer), args.Error(1)
}

type MockPathMapper struct {
	mock.Mock
}

func (m *MockPathMapper) ResolveLocalExisting(path string) (string, bool) {
	return path, true
}

func (m *MockPathMapper) ResolveLocalUnsafe(path string) (string, bool) {
	return path, true
}

type MockProber struct {
	mock.Mock
}

func (m *MockProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*vod.StreamInfo), args.Error(1)
}

type dummyRunner struct{}

func (d *dummyRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	return nil, nil
}

// Helpers

func setupService(t *testing.T) (Service, *MockOWIClient, *vod.Manager, *MockProber) {
	cfg := &config.AppConfig{
		RecordingRoots: map[string]string{"hdd": "/media/hdd/movie"},
	}
	owi := new(MockOWIClient)
	prober := new(MockProber)
	mapper := new(MockPathMapper)

	// Use real VOD Manager to test interaction with metadata/jobs
	mgr := vod.NewManager(&dummyRunner{}, prober, mapper)

	// Mock resolver? We can pass nil for List tests, but Stream tests need it?
	svc := NewService(cfg, mgr, &mockResolver{}, owi, nil)
	return svc, owi, mgr, prober
}

type mockResolver struct{}

func (m *mockResolver) Resolve(ctx context.Context, ref string, intent PlaybackIntent, profile PlaybackProfile) (PlaybackInfoResult, error) {
	return PlaybackInfoResult{}, nil
}

// Table A Tests (Read Path)

func TestDurationTruth_Read_StoreWins(t *testing.T) {
	svc, owi, mgr, prober := setupService(t)
	ctx := context.Background()

	// Setup: Store has 60m, Metadata has 50m
	ref := "1:0:1:ABCD:1:1:C00000:0:0:0:/media/hdd/movie/test.ts"
	storeLength := "60 min" // Parses to 3600

	mgr.SeedMetadata(ref, vod.Metadata{
		State:     vod.ArtifactStateReady,
		Duration:  3000, // 50 min
		UpdatedAt: time.Now().Unix(),
	})

	owi.On("GetLocations", ctx).Return([]OWILocation{}, nil)
	owi.On("GetTimers", ctx).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", ctx, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{ServiceRef: ref, Title: "Store Win", Length: storeLength},
		},
	}, nil)

	// Call List
	res, err := svc.List(ctx, ListInput{})
	assert.NoError(t, err)
	assert.Len(t, res.Recordings, 1)

	// Assert A1: Store Wins (3600)
	assert.NotNil(t, res.Recordings[0].DurationSeconds)
	assert.Equal(t, int64(3600), *res.Recordings[0].DurationSeconds)

	// Assert No Probe Triggered (MockProber should not be called)
	prober.AssertNotCalled(t, "Probe")
}

func TestDurationTruth_Read_ProbeFallback(t *testing.T) {
	svc, owi, mgr, prober := setupService(t)
	ctx := context.Background()

	ref := "1:0:1:ABCD:1:1:C00000:0:0:0:/media/hdd/movie/test.ts"

	// Setup: Store invalid/missing, Metadata valid
	mgr.SeedMetadata(ref, vod.Metadata{
		State:     vod.ArtifactStateReady,
		Duration:  3000,
		UpdatedAt: time.Now().Unix(),
	})

	owi.On("GetLocations", ctx).Return([]OWILocation{}, nil)
	owi.On("GetTimers", ctx).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", ctx, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{ServiceRef: ref, Title: "Probe Fallback", Length: ""}, // Empty length
		},
	}, nil)

	res, err := svc.List(ctx, ListInput{})
	assert.NoError(t, err)

	// Assert A2: Probe Fallback
	assert.NotNil(t, res.Recordings[0].DurationSeconds)
	assert.Equal(t, int64(3000), *res.Recordings[0].DurationSeconds)

	prober.AssertNotCalled(t, "Probe")
}

func TestDurationTruth_Read_Unknown(t *testing.T) {
	svc, owi, _, prober := setupService(t)
	ctx := context.Background()

	ref := "1:0:1:ABCD:1:1:C00000:0:0:0:/media/hdd/movie/test.ts"

	// Setup: Store invalid, Metadata missing/unknown
	// (No metadata in mgr)

	owi.On("GetLocations", ctx).Return([]OWILocation{}, nil)
	owi.On("GetTimers", ctx).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", ctx, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{ServiceRef: ref, Title: "Unknown", Length: "-1"}, // Invalid
		},
	}, nil)

	res, err := svc.List(ctx, ListInput{})
	assert.NoError(t, err)

	// Assert A3: Nil Duration (Soft Fail for List)
	assert.Nil(t, res.Recordings[0].DurationSeconds)

	prober.AssertNotCalled(t, "Probe")
}

func TestDurationTruth_Read_BuildingGate(t *testing.T) {
	svc, owi, mgr, _ := setupService(t)
	ctx := context.Background()

	ref := "1:0:1:ABCD:1:1:C00000:0:0:0:/media/hdd/movie/test.ts"

	// Setup: Store valid (3600), but Metadata says PREPARING
	// A4: Building State Guard overrides Store? "Regardless of Store/Probe"

	mgr.SeedMetadata(ref, vod.Metadata{
		State:    vod.ArtifactStatePreparing,
		Duration: 3000, // Even if it has a duration
	})

	owi.On("GetLocations", ctx).Return([]OWILocation{}, nil)
	owi.On("GetTimers", ctx).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", ctx, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{ServiceRef: ref, Title: "Building", Length: "60 min"},
		},
	}, nil)

	res, err := svc.List(ctx, ListInput{})
	assert.NoError(t, err)

	// Assert A4: Nil Duration (Building Gate)
	assert.Nil(t, res.Recordings[0].DurationSeconds, "Duration must be nil when Building/Preparing")
}

func TestDurationTruth_Read_ParseErrorMetrics(t *testing.T) {
	svc, owi, mgr, _ := setupService(t)
	ctx := context.Background()
	ref := "1:0:1:ABCD:1:1:C00000:0:0:0:/media/hdd/movie/test.ts"

	// Setup: Malformed store
	mgr.SeedMetadata(ref, vod.Metadata{
		State:    vod.ArtifactStateReady,
		Duration: 3000,
	})

	owi.On("GetLocations", ctx).Return([]OWILocation{}, nil)
	owi.On("GetTimers", ctx).Return([]OWITimer{}, nil)
	owi.On("GetRecordings", ctx, "/media/hdd/movie").Return(OWIRecordingsList{
		Result: true,
		Movies: []OWIMovie{
			{ServiceRef: ref, Title: "Parse Error", Length: "invalid"},
		},
	}, nil)

	res, err := svc.List(ctx, ListInput{})
	assert.NoError(t, err)

	// Assert A5: Fallback works
	assert.NotNil(t, res.Recordings[0].DurationSeconds)
	assert.Equal(t, int64(3000), *res.Recordings[0].DurationSeconds)
}
