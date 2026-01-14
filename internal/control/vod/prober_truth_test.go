package vod

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks

type MockInfraProber struct {
	mock.Mock
}

func (m *MockInfraProber) Probe(ctx context.Context, path string) (*StreamInfo, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*StreamInfo), args.Error(1)
}

type MockMapper struct{}

func (m *MockMapper) ResolveLocalExisting(p string) (string, bool) { return p, true }

func setupManager(t *testing.T) (*Manager, *MockInfraProber, string) {
	prober := new(MockInfraProber)
	mgr := NewManager(&mockRunner{}, prober, &MockMapper{})

	f, err := os.CreateTemp("", "test*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	return mgr, prober, f.Name()
}

// Tests

func TestTruth_Write_B1_Success(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	// Mock successful probe > 0
	info := &StreamInfo{
		Video:     VideoStreamInfo{Duration: 60.5, CodecName: "h264"},
		Audio:     AudioStreamInfo{CodecName: "aac"},
		Container: "mov,mp4",
	}
	prober.On("Probe", mock.Anything, path).Return(info, nil)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(req)

	assert.NoError(t, err)

	// Verify Metadata Updated
	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStateReady, meta.State)
	assert.Equal(t, int64(61), meta.Duration)
}

func TestTruth_Write_B2_ZeroDuration_Guard(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	// Setup: Existing valid metadata
	mgr.SeedMetadata(ref, Metadata{
		State:     ArtifactStateReady,
		Duration:  3600,
		UpdatedAt: time.Now().Unix(),
	})

	// Mock probe returning 0 duration (but valid file?)
	info := &StreamInfo{
		Video: VideoStreamInfo{Duration: 0, CodecName: "h264"},
	}
	prober.On("Probe", mock.Anything, path).Return(info, nil)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(req)

	// Expectation: Error returned (Invalid Duration)
	assert.Error(t, err)

	// Verify Metadata NOT overwritten with 0
	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, int64(3600), meta.Duration)
}

func TestTruth_Write_B3_Timeout_Mapping(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	// Mock Timeout
	prober.On("Probe", mock.Anything, path).Return(nil, context.DeadlineExceeded)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(req)

	// Expectation: specific error, State should NOT be Ready
	assert.Error(t, err)

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStatePreparing, meta.State)
}

// B4: Corrupt -> Failed
func TestTruth_Write_B4_Corrupt(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	prober.On("Probe", mock.Anything, path).Return(nil, errors.New("corrupt file"))

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(req)

	assert.Error(t, err)

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStateFailed, meta.State)
}
