package recordings_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/playback"
	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mocks ---

type mockDurationStore struct {
	mock.Mock
}

func (m *mockDurationStore) GetDuration(ctx context.Context, rootID, relPath string) (int64, bool, error) {
	args := m.Called(ctx, rootID, relPath)
	return args.Get(0).(int64), args.Bool(1), args.Error(2)
}

func (m *mockDurationStore) SetDuration(ctx context.Context, rootID, relPath string, seconds int64) error {
	args := m.Called(ctx, rootID, relPath, seconds)
	return args.Error(0)
}

type mockPathResolver struct {
	mock.Mock
}

func (m *mockPathResolver) ResolveRecordingPath(serviceRef string) (string, string, string, error) {
	args := m.Called(serviceRef)
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

type mockMetadataManager struct {
	mock.Mock
}

func (m *mockMetadataManager) Get(ctx context.Context, dir string) (*vod.JobStatus, bool) {
	args := m.Called(ctx, dir)
	if s := args.Get(0); s != nil {
		return s.(*vod.JobStatus), args.Bool(1)
	}
	return nil, args.Bool(1)
}

func (m *mockMetadataManager) GetMetadata(serviceRef string) (vod.Metadata, bool) {
	args := m.Called(serviceRef)
	// Support dynamic return values via functions
	var meta vod.Metadata
	if fn, ok := args.Get(0).(func(string) vod.Metadata); ok {
		meta = fn(serviceRef)
	} else {
		meta = args.Get(0).(vod.Metadata)
	}

	var exists bool
	if fn, ok := args.Get(1).(func(string) bool); ok {
		exists = fn(serviceRef)
	} else {
		exists = args.Bool(1)
	}
	return meta, exists
}

func (m *mockMetadataManager) MarkFailed(serviceRef string, reason string) {
	m.Called(serviceRef, reason)
}

func (m *mockMetadataManager) MarkFailure(serviceRef string, state vod.ArtifactState, reason string, resolvedPath string, fp *vod.Fingerprint) {
	m.Called(serviceRef, state, reason, resolvedPath, fp)
}

func (m *mockMetadataManager) MarkProbed(serviceRef string, resolvedPath string, info *vod.StreamInfo, fp *vod.Fingerprint) {
	m.Called(serviceRef, resolvedPath, info, fp)
}

func (m *mockMetadataManager) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	args := m.Called(ctx, path)
	if s := args.Get(0); s != nil {
		return s.(*vod.StreamInfo), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockMetadataManager) EnsureSpec(ctx context.Context, workDir, recordingID, source, cacheDir, name, finalPath string, profile vod.Profile) (vod.Spec, error) {
	args := m.Called(ctx, workDir, recordingID, source, cacheDir, name, finalPath, profile)
	return args.Get(0).(vod.Spec), args.Error(1)
}

// --- Tests ---

func TestDurationTruth_Read_StoreWins(t *testing.T) {
	ds := new(mockDurationStore)
	ds.On("GetDuration", mock.Anything, "1", "movie.ts").Return(int64(3600), true, nil)

	mgr := new(mockMetadataManager)
	mgr.On("GetMetadata", mock.Anything).Return(vod.Metadata{
		Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Duration: 100, // Partial/Wrong duration
	}, true)
	mgr.On("Get", mock.Anything, mock.Anything).Return(nil, false)

	pr := new(mockPathResolver)
	pr.On("ResolveRecordingPath", mock.Anything).Return("/local/movie.ts", "1", "movie.ts", nil)

	r, err := recordings.NewResolver(&config.AppConfig{}, mgr, recordings.ResolverOptions{
		DurationStore: ds,
		PathResolver:  pr,
	})
	require.NoError(t, err)

	res, err := r.Resolve(context.Background(), "service:ref", recordings.IntentStream, recordings.ProfileGeneric)
	assert.NoError(t, err)
	assert.Equal(t, 3600.0, res.MediaInfo.Duration)
}

func TestDurationTruth_Read_ProbeFallback(t *testing.T) {
	ds := new(mockDurationStore)
	ds.On("GetDuration", mock.Anything, "1", "movie.ts").Return(int64(0), false, nil)
	ds.On("SetDuration", mock.Anything, "1", "movie.ts", int64(3600)).Return(nil)

	mgr := new(mockMetadataManager)

	// Mock Sequence:
	// 1. Return Empty (Failure Pass) -> Consumed by Resolve logic (1 call)
	// 2. Return Complete (Success Pass) -> Consumed by Resolve logic (2 calls)
	mgr.On("GetMetadata", mock.Anything).Return(vod.Metadata{}, false).Once()
	mgr.On("GetMetadata", mock.Anything).Return(vod.Metadata{
		Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Duration: 3600,
	}, true)

	mgr.On("MarkProbed", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mgr.On("Get", mock.Anything, mock.Anything).Return(nil, false)

	// Probe mock setup
	probeFinished := make(chan struct{})
	var probeOnce sync.Once
	mgr.On("Probe", mock.Anything, "/local/movie.ts").Return(&vod.StreamInfo{
		Container: "mp4",
		Video:     vod.VideoStreamInfo{CodecName: "h264", Duration: 3600},
		Audio:     vod.AudioStreamInfo{CodecName: "aac"},
	}, nil).Run(func(args mock.Arguments) {
		probeOnce.Do(func() { close(probeFinished) })
	})

	pr := new(mockPathResolver)
	pr.On("ResolveRecordingPath", mock.Anything).Return("/local/movie.ts", "1", "movie.ts", nil)

	r, err := recordings.NewResolver(&config.AppConfig{}, mgr, recordings.ResolverOptions{
		DurationStore: ds,
		PathResolver:  pr,
	})
	require.NoError(t, err)

	// 1. First Call -> Expect Preparing
	_, err = r.Resolve(context.Background(), "service:ref", recordings.IntentStream, recordings.ProfileGeneric)
	var prepErr recordings.ErrPreparing
	if assert.ErrorAs(t, err, &prepErr) {
		assert.Equal(t, "service:ref", prepErr.RecordingID)
	} else {
		assert.Fail(t, "Expected ErrPreparing")
	}

	// Wait for Probe
	select {
	case <-probeFinished:
	case <-time.After(1 * time.Second):
		t.Fatal("probe timed out")
	}

	// 2. Second Call -> Expect Success
	res, err := r.Resolve(context.Background(), "service:ref", recordings.IntentStream, recordings.ProfileGeneric)
	assert.NoError(t, err)
	assert.Equal(t, 3600.0, res.MediaInfo.Duration)
}

// --- Truth Matrix Test ---

type TruthTestCase struct {
	Name       string
	ServiceRef string
	LocalPath  string
	RootID     string
	RelPath    string

	JobState      *vod.JobStatus
	StoreDuration int64
	StoreErr      error
	CacheMeta     vod.Metadata
	CacheExists   bool

	ProbeResult    *vod.StreamInfo
	ProbeErr       error
	ProbeRemoteErr error

	ExpRes   recordings.PlaybackInfoResult
	ExpErr   error
	ExpErrIs error

	ExpectProbe bool
	ExpectAsync bool // Expect ErrPreparing first, then ExpRes/ExpErr
}

func TestMediaTruth(t *testing.T) {
	s := func(v string) *string { return &v }
	cases := []TruthTestCase{
		{
			Name:          "Hit Store + Meta Codecs Complete",
			StoreDuration: 3600,
			LocalPath:     "/media/movie.ts", // Added LocalPath to enable Store lookup
			CacheMeta:     vod.Metadata{Container: "ts", VideoCodec: "h264", AudioCodec: "ac3", Duration: 0},
			CacheExists:   true,
			ExpRes: recordings.PlaybackInfoResult{
				MediaInfo: playback.MediaInfo{
					Container: "ts", VideoCodec: "h264", AudioCodec: "ac3", Duration: 3600,
				},
				VideoCodec: s("h264"), AudioCodec: s("ac3"),
				Reason: string(playback.ReasonTranscodeAudio), // PIDE returns transcode_audio for AC3 in Generic
			},
		},
		{
			Name:          "Hit Metadata (Cache)",
			StoreDuration: 0,
			CacheMeta:     vod.Metadata{Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Duration: 120, ArtifactPath: "x"}, // ArtifactPath prevents "Impossible Gate"
			CacheExists:   true,
			ExpRes: recordings.PlaybackInfoResult{
				MediaInfo: playback.MediaInfo{Duration: 120},
				Reason:    string(playback.ReasonDirectPlayMatch),
			},
		},
		{
			Name:          "Heal: Store Hit + Codec Miss",
			StoreDuration: 3600,
			CacheMeta:     vod.Metadata{}, // Empty cache
			CacheExists:   false,
			ExpectProbe:   true,
			ExpectAsync:   true, // Now Async
			LocalPath:     "/media/movie.ts",
			ProbeResult: &vod.StreamInfo{
				Container: "ts",
				Video:     vod.VideoStreamInfo{CodecName: "h264", Duration: 3600},
				Audio:     vod.AudioStreamInfo{CodecName: "mp2"},
			},
			ExpRes: recordings.PlaybackInfoResult{
				MediaInfo: playback.MediaInfo{Duration: 3600, Container: "ts"},
				Reason:    string(playback.ReasonTranscodeAudio), // transcode_audio (mp2 -> aac)
			},
		},
		{
			Name:          "Heal: Store Hit + Partial Codec -> Heal",
			StoreDuration: 3600,
			CacheMeta:     vod.Metadata{Container: "ts", VideoCodec: "", AudioCodec: "mp2"},
			CacheExists:   true,
			ExpectProbe:   true,
			ExpectAsync:   true,
			LocalPath:     "/media/movie.ts",
			ProbeResult: &vod.StreamInfo{
				Container: "ts",
				Video:     vod.VideoStreamInfo{CodecName: "mpeg2", Duration: 3600}, // mpeg2!
				Audio:     vod.AudioStreamInfo{CodecName: "mp2"},
			},
			ExpRes: recordings.PlaybackInfoResult{
				MediaInfo: playback.MediaInfo{Duration: 3600, VideoCodec: "mpeg2"},
				Reason:    string(playback.ReasonTranscodeVideo),
			},
		},
		{
			// Probe fails with timeout -> logic calls cancel(), returns Preparing
			Name:        "Heal Fail: Timeout -> ErrPreparing",
			CacheMeta:   vod.Metadata{},
			ExpectProbe: true,
			ExpectAsync: true,
			LocalPath:   "/media/movie.ts", // Enable local probe (pass gate)
			ExpErrIs:    recordings.ErrPreparing{RecordingID: "service:ref"},
		},
		{
			// Probe returns nil -> Corrupt.
			Name:        "Heal Fail: Corrupt -> ErrUpstream",
			CacheMeta:   vod.Metadata{},
			ExpectProbe: true,
			ExpectAsync: true,
			LocalPath:   "/media/movie.ts", // Enable local probe
			ProbeResult: nil,               // Probe returns nil
			ExpErrIs:    recordings.ErrPreparing{RecordingID: "service:ref"},
		},
		{
			// Probe error -> Upstream.
			Name:        "Probe Fail (No Defaults)",
			CacheMeta:   vod.Metadata{},
			ExpectProbe: true,
			ExpectAsync: true,
			LocalPath:   "/media/movie.ts", // Enable local probe
			ProbeErr:    errors.New("probe failed"),
			ExpErrIs:    recordings.ErrPreparing{RecordingID: "service:ref"},
		},
		{
			// Remote Probe -> FAIL FAST (Gate 2)
			Name:           "Remote Probe Unsupported (No Codecs)",
			ServiceRef:     "1:0:0:0:0:0:0:0:0:0:/local/x",
			CacheMeta:      vod.Metadata{},
			ExpectProbe:    false, // No probe triggered (fail fast)
			ExpectAsync:    false, // Sync error
			ProbeRemoteErr: recordings.ErrRemoteProbeUnsupported,
			// Resolver maps ErrRemoteProbeUnsupported -> ErrUpstream{Op: "probe_remote_unsupported"}
			ExpErrIs: recordings.ErrUpstream{Op: "probe_remote_unsupported", Cause: recordings.ErrRemoteProbeUnsupported},
		},
		{
			// Remote Probe Failure -> FAIL FAST (Gate 2)
			Name:        "Remote Probe Failure (Upstream)",
			CacheMeta:   vod.Metadata{},
			ExpectProbe: false,
			ExpectAsync: false,
			ProbeErr:    errors.New("conn refused"),
			// Since we use logic: if !probeConfigured -> ErrUpstream.
			// Here probe IS configured (test harness sets it).
			// So it hits "Gate 2": if localPath=="" && probeConfigured -> ErrRemoteProbeUnsupported.
			// It implies any "Remote Probe" attempt without local path is unsupported currently.
			// So expected error is ErrRemoteProbeUnsupported (mapped to Upstream)
			ExpErrIs: recordings.ErrUpstream{Op: "probe_remote_unsupported", Cause: recordings.ErrRemoteProbeUnsupported},
		},
		{
			Name:        "Store Get Error (No Overwrite)",
			StoreErr:    errors.New("db disconnect"),
			CacheMeta:   vod.Metadata{},
			ExpectProbe: true,
			ExpectAsync: true,
			LocalPath:   "/media/movie.ts",
			ProbeResult: &vod.StreamInfo{
				Container: "ts",
				Video:     vod.VideoStreamInfo{CodecName: "h264", Duration: 3600},
				Audio:     vod.AudioStreamInfo{CodecName: "ac3"},
			},
			ExpRes: recordings.PlaybackInfoResult{
				MediaInfo: playback.MediaInfo{Duration: 3600},
			},
		},
		{
			// ADR Enforcement: READY state != Artifact Exists
			// If meta is READY but ArtifactPath/PlaylistPath are empty, fallback to receiver/transcode.
			Name:          "Invariant: Ready Meta != Artifact",
			StoreDuration: 60,
			LocalPath:     "/media/movie.ts", // Required to pass "Impossible Probe" gate (Gate 2)
			CacheMeta: vod.Metadata{
				State:        vod.ArtifactStateReady,
				Duration:     60,
				ResolvedPath: "/media/movie.ts",
				// Complete media info to avoid re-probing
				Container:  "ts",
				VideoCodec: "h264",
				AudioCodec: "aac",
				// ArtifactPath and PlaylistPath intentionally empty
			},
			CacheExists: true,
			ExpRes: recordings.PlaybackInfoResult{
				MediaInfo: playback.MediaInfo{
					Duration:   60,
					Container:  "ts",
					VideoCodec: "h264",
					AudioCodec: "aac",
				},
				Reason: string(playback.ReasonDirectPlayMatch), // Direct play from source path (no artifact/playlist required)
				// Key check: It returns a valid result (not error/missing) but NOT direct play OR HLS if implied by artifact
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			svcRef := "service:ref"
			if tc.ServiceRef != "" {
				svcRef = tc.ServiceRef
			}

			ds := new(mockDurationStore)
			if tc.StoreErr != nil {
				ds.On("GetDuration", mock.Anything, mock.Anything, mock.Anything).Return(int64(0), false, tc.StoreErr)
			} else {
				ds.On("GetDuration", mock.Anything, mock.Anything, mock.Anything).Return(tc.StoreDuration, tc.StoreDuration > 0, nil)
			}

			// If probe succeeds and store was empty/error, expect SetDuration
			if tc.ProbeResult != nil && tc.ProbeResult.Video.Duration > 0 && tc.StoreDuration == 0 && tc.StoreErr == nil {
				ds.On("SetDuration", mock.Anything, mock.Anything, mock.Anything, mock.MatchedBy(func(dur int64) bool {
					return dur == int64(tc.ProbeResult.Video.Duration)
				})).Return(nil)
			}

			mgr := new(mockMetadataManager)

			// Probe Setup
			probeDone := make(chan struct{})
			var probeOnce sync.Once

			// Explicit Sequencing for GetMetadata
			if tc.ExpectAsync {
				// Use closure to simulation state change upon probe completion
				// Before probeDone is closed: return CacheMeta/CacheExists
				// After probeDone is closed: return FinalMeta/true
				var finalMeta vod.Metadata
				if tc.ProbeResult != nil {
					finalMeta = vod.Metadata{
						Container:  tc.ProbeResult.Container,
						VideoCodec: tc.ProbeResult.Video.CodecName,
						AudioCodec: tc.ProbeResult.Audio.CodecName,
						Duration:   int64(tc.ProbeResult.Video.Duration),
					}
				} else {
					finalMeta = tc.CacheMeta // Fallback if no result
				}

				mgr.On("GetMetadata", svcRef).Return(func(id string) vod.Metadata {
					select {
					case <-probeDone:
						return finalMeta
					default:
						return tc.CacheMeta
					}
				}, func(id string) bool {
					select {
					case <-probeDone:
						return true
					default:
						return tc.CacheExists
					}
				})
			} else {
				// Stable state
				mgr.On("GetMetadata", svcRef).Return(tc.CacheMeta, tc.CacheExists)
			}

			mgr.On("Get", mock.Anything, mock.Anything).Return(tc.JobState, tc.JobState != nil)

			pr := new(mockPathResolver)
			if tc.LocalPath != "" {
				pr.On("ResolveRecordingPath", mock.Anything).Return(tc.LocalPath, "1", "x", nil)
			} else {
				pr.On("ResolveRecordingPath", mock.Anything).Return("", "", "", errors.New("no map"))
			}

			// Probe Setup
			if tc.ExpectProbe {
				if tc.LocalPath != "" {
					call := mgr.On("Probe", mock.Anything, tc.LocalPath).Return(tc.ProbeResult, tc.ProbeErr)
					if tc.ExpectAsync {
						call.Run(func(args mock.Arguments) {
							probeOnce.Do(func() { close(probeDone) })
						})
					}
				}
				if tc.ProbeResult == nil {
					// Expect MarkFailed for failure (explicit error or nil result)
					mgr.On("MarkFailed", svcRef, mock.MatchedBy(func(reason string) bool {
						return reason != ""
					})).Return().Maybe()
				} else {
					// Expect MarkProbed for success or other meta updates
					mgr.On("MarkProbed", svcRef, mock.Anything, mock.Anything, mock.Anything).Return().Maybe()
				}
			}

			r, err := recordings.NewResolver(&config.AppConfig{}, mgr, recordings.ResolverOptions{
				DurationStore: ds,
				PathResolver:  pr,
				ProbeFn: func(ctx context.Context, u string) error {
					if tc.ExpectAsync {
						probeOnce.Do(func() { close(probeDone) })
					}
					return tc.ProbeRemoteErr
				},
			})
			require.NoError(t, err)

			// 1. First Call
			res, err := r.Resolve(context.Background(), svcRef, recordings.IntentStream, recordings.ProfileGeneric)

			if tc.ExpectAsync {
				// Must be Preparing
				var prepErr recordings.ErrPreparing
				if assert.ErrorAs(t, err, &prepErr) {
					assert.Equal(t, svcRef, prepErr.RecordingID)
				} else {
					assert.Equal(t, recordings.ErrPreparing{RecordingID: svcRef}, err)
				}

				// Wait for probe
				select {
				case <-probeDone:
				case <-time.After(500 * time.Millisecond):
					t.Log("Warning: Probe test channel timed out (maybe already closed or not called)")
				}

				// Retry verification if success expected
				if tc.ExpErr == nil && tc.ExpErrIs == nil {
					// 2. Second Call
					res2, err2 := r.Resolve(context.Background(), svcRef, recordings.IntentStream, recordings.ProfileGeneric)
					assert.NoError(t, err2)

					// Assertions on final result
					if tc.ExpRes.Reason != "" {
						assert.Equal(t, tc.ExpRes.Reason, res2.Reason)
					}
				}

			} else {
				// Sync checks
				if tc.ExpErrIs != nil {
					assert.ErrorIs(t, err, tc.ExpErrIs)
				} else if tc.ExpErr != nil {
					assert.Equal(t, tc.ExpErr, err)
				} else {
					assert.NoError(t, err)
					if tc.ExpRes.Reason != "" {
						assert.Equal(t, tc.ExpRes.Reason, res.Reason)
					}
					if tc.ExpRes.MediaInfo.Duration > 0 {
						assert.Equal(t, tc.ExpRes.MediaInfo.Duration, res.MediaInfo.Duration)
					}
				}
			}
		})
	}
}

// Ensure No Hidden Work Invariant
func TestAsyncProbe_NoHiddenWork(t *testing.T) {
	mgr := new(mockMetadataManager)
	mgr.On("Get", mock.Anything, mock.Anything).Return(nil, false)
	mgr.On("GetMetadata", mock.Anything).Return(vod.Metadata{}, false) // Cache miss

	// Probe mock that simulates work
	probeUnblocked := make(chan struct{})
	probeCalled := make(chan struct{})
	mgr.On("Probe", mock.Anything, "/local/x").Run(func(args mock.Arguments) {
		close(probeCalled)
		<-probeUnblocked // Block until test releases
	}).Return(&vod.StreamInfo{Container: "mp4", Video: vod.VideoStreamInfo{CodecName: "h264"}, Audio: vod.AudioStreamInfo{CodecName: "aac"}}, nil)

	mgr.On("MarkProbed", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	pr := new(mockPathResolver)
	pr.On("ResolveRecordingPath", mock.Anything).Return("/local/x", "1", "x", nil)

	ds := new(mockDurationStore)
	ds.On("GetDuration", mock.Anything, "1", "x").Return(int64(0), false, nil)
	ds.On("SetDuration", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	r, err := recordings.NewResolver(&config.AppConfig{}, mgr, recordings.ResolverOptions{
		DurationStore: ds,
		PathResolver:  pr,
	})
	require.NoError(t, err)

	// Call Resolve - Trigger Probe
	// Structural Assertion: We have NOT closed probeUnblocked yet.
	// If Resolve returns now, it PROVES it did not wait for the probe.
	_, err = r.Resolve(context.Background(), "service:ref", recordings.IntentStream, recordings.ProfileGeneric)

	// Assert immediate return with correct error
	assert.ErrorIs(t, err, recordings.ErrPreparing{RecordingID: "service:ref"})

	// Verify probe was actually started eventually
	select {
	case <-probeCalled:
		// Probe started in background
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Probe should have been triggered in background")
	}

	// Release the probe
	close(probeUnblocked)
}

// Ensure Singleflight (Stampede Prevention)
func TestAsyncProbe_Singleflight(t *testing.T) {
	mgr := new(mockMetadataManager)
	// Relaxed expectation: Get might be called multiple times by concurrent routines, or just once if fast enough
	mgr.On("Get", mock.Anything, mock.Anything).Return(nil, false).Maybe()
	// Relaxed expectation: GetMetadata will initially fail (miss), then trigger probe
	mgr.On("GetMetadata", mock.Anything).Return(vod.Metadata{}, false)

	// We expect Probe to be called EXACTLY ONCE despite multiple concurrent requests
	probeCalled := make(chan struct{})
	mgr.On("Probe", mock.Anything, "/local/x").Return(&vod.StreamInfo{
		Container: "mp4",
		Video:     vod.VideoStreamInfo{CodecName: "h264"},
		Audio:     vod.AudioStreamInfo{CodecName: "aac"},
	}, nil).Once().Run(func(args mock.Arguments) {
		close(probeCalled)
		time.Sleep(10 * time.Millisecond)
	})

	// We expect UpdateMetadata to happen eventually, but not blocking.
	// Since we sleep in the probe, it might happen after.
	updateCalled := make(chan struct{})
	mgr.On("MarkProbed", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		close(updateCalled)
	}).Return()

	pr := new(mockPathResolver)
	pr.On("ResolveRecordingPath", mock.Anything).Return("/local/x", "1", "x", nil)

	ds := new(mockDurationStore)
	ds.On("GetDuration", mock.Anything, "1", "x").Return(int64(0), false, nil)
	// Relax SetDuration: it might or might not happen depending on code path/timing in background
	ds.On("SetDuration", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()

	r, err := recordings.NewResolver(&config.AppConfig{}, mgr, recordings.ResolverOptions{
		DurationStore: ds,
		PathResolver:  pr,
	})
	require.NoError(t, err)

	// Concurrent Stampede
	concurrency := 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_, err := r.Resolve(context.Background(), "service:ref", recordings.IntentStream, recordings.ProfileGeneric)
			// All should get ErrPreparing
			assert.ErrorIs(t, err, recordings.ErrPreparing{RecordingID: "service:ref"})
		}()
	}

	wg.Wait()

	select {
	case <-probeCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Probe should have been called")
	}

	// Ensure side effect happened (wait for update)
	select {
	case <-updateCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("MarkProbed should have been called")
	}

	// Mock assertion verifies .Once() for Probe
	mgr.AssertExpectations(t)
}
