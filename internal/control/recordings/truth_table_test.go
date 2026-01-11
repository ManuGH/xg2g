package recordings

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// Mocks
type mockMetadataManager struct {
	getMetadataFunc    func(serviceRef string) (vod.Metadata, bool)
	updateMetadataFunc func(serviceRef string, meta vod.Metadata)
	probeFunc          func(ctx context.Context, path string) (*vod.StreamInfo, error)
	getFunc            func(ctx context.Context, dir string) (*vod.JobStatus, bool)

	// Call counters
	updateMetadataCalls int
	probeCalls          int
}

func (m *mockMetadataManager) GetMetadata(serviceRef string) (vod.Metadata, bool) {
	if m.getMetadataFunc != nil {
		return m.getMetadataFunc(serviceRef)
	}
	return vod.Metadata{}, false
}

func (m *mockMetadataManager) UpdateMetadata(serviceRef string, meta vod.Metadata) {
	m.updateMetadataCalls++
	if m.updateMetadataFunc != nil {
		m.updateMetadataFunc(serviceRef, meta)
	}
}

func (m *mockMetadataManager) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	m.probeCalls++
	if m.probeFunc != nil {
		return m.probeFunc(ctx, path)
	}
	return nil, errors.New("unexpected probe call")
}

func (m *mockMetadataManager) Get(ctx context.Context, dir string) (*vod.JobStatus, bool) {
	if m.getFunc != nil {
		return m.getFunc(ctx, dir)
	}
	return nil, false
}

func (m *mockMetadataManager) EnsureSpec(ctx context.Context, workDir, recordingID, source, cacheDir, name, finalPath string, profile vod.Profile) (vod.Spec, error) {
	return vod.Spec{}, nil // Not used in duration logic
}

type mockDurationStore struct {
	getDurationFunc func(ctx context.Context, rootID, relPath string) (int64, bool, error)
	setDurationFunc func(ctx context.Context, rootID, relPath string, duration int64) error

	// Call counters
	setCalls int
}

func (s *mockDurationStore) GetDuration(ctx context.Context, rootID, relPath string) (int64, bool, error) {
	if s.getDurationFunc != nil {
		return s.getDurationFunc(ctx, rootID, relPath)
	}
	return 0, false, nil
}

func (s *mockDurationStore) SetDuration(ctx context.Context, rootID, relPath string, duration int64) error {
	s.setCalls++
	if s.setDurationFunc != nil {
		return s.setDurationFunc(ctx, rootID, relPath, duration)
	}
	return nil
}

type mockPathResolver struct {
	resolveFunc func(serviceRef string) (localPath, rootID, relPath string, err error)
}

func (r *mockPathResolver) ResolveRecordingPath(serviceRef string) (localPath, rootID, relPath string, err error) {
	if r.resolveFunc != nil {
		return r.resolveFunc(serviceRef)
	}
	return "", "", "", nil
}

// TestMediaTruth implements the Mechanical Verification Matrix for Deliverable #4
func TestMediaTruth(t *testing.T) {
	tests := []struct {
		name string

		// Inputs
		storeDuration int64
		storeError    error

		metaDuration  int64
		metaContainer string
		metaVCodec    string
		metaACodec    string
		metaExists    bool

		probeResult *vod.StreamInfo
		probeError  error

		resolveRootID string
		resolvePath   string

		storeSetError error

		// Expected Output
		wantDuration float64
		wantReason   string
		wantErrorIs  error

		// Side Effects
		wantProbeCalls    int
		wantStoreSetCalls int
		wantMetaUpdates   int
	}{
		// --- Duration Truth Base Cases (Regression Checks) ---
		{
			name:              "Hit Store + Meta Codecs Complete",
			storeDuration:     50,
			resolveRootID:     "root1",
			resolvePath:       "rel/path",
			metaDuration:      0,
			metaContainer:     "mpegts",
			metaVCodec:        "h264",
			metaACodec:        "aac",
			metaExists:        true,
			wantDuration:      50,
			wantReason:        "store_duration__metadata_codecs",
			wantProbeCalls:    0,
			wantStoreSetCalls: 0,
			wantMetaUpdates:   0,
		},
		{
			name:              "Hit Metadata (Cache)",
			storeDuration:     0,
			metaDuration:      60,
			metaContainer:     "mpegts",
			metaVCodec:        "h264",
			metaACodec:        "aac",
			metaExists:        true,
			wantDuration:      60,
			wantReason:        "resolved_via_metadata",
			wantProbeCalls:    0,
			wantStoreSetCalls: 0,
			wantMetaUpdates:   0,
		},

		// --- Codec Truth & Heal Cases ---
		{
			name:          "Heal: Store Hit + Codec Miss",
			storeDuration: 50,
			resolveRootID: "root1",
			resolvePath:   "rel/path",
			metaDuration:  0,
			metaExists:    false, // Or true but empty codecs
			probeResult: &vod.StreamInfo{
				Video:     vod.VideoStreamInfo{Duration: 70.0, CodecName: "h265"}, // Duration from probe ignored for truth, used for codecs
				Audio:     vod.AudioStreamInfo{CodecName: "ac3"},
				Container: "mp4",
			},
			wantDuration:      50, // Keep store duration
			wantReason:        "store_duration__probed_codecs",
			wantProbeCalls:    1,
			wantStoreSetCalls: 0, // Don't persist duration if we trusted store
			wantMetaUpdates:   1, // Persist codecs
		},
		{
			name:          "Heal: Store Hit + Partial Codec -> Heal",
			storeDuration: 50,
			resolveRootID: "root1",
			resolvePath:   "rel/path",
			metaDuration:  0,
			metaContainer: "mpegts",
			metaVCodec:    "h264",
			metaACodec:    "", // Missing audio
			metaExists:    true,
			probeResult: &vod.StreamInfo{
				Video:     vod.VideoStreamInfo{Duration: 70.0, CodecName: "h264"},
				Audio:     vod.AudioStreamInfo{CodecName: "aac"},
				Container: "mpegts",
			},
			wantDuration:      50,
			wantReason:        "store_duration__probed_codecs",
			wantProbeCalls:    1,
			wantStoreSetCalls: 0,
			wantMetaUpdates:   1,
		},
		{
			name:           "Heal Fail: Timeout -> ErrPreparing",
			storeDuration:  50,
			resolveRootID:  "root1",
			resolvePath:    "rel/path",
			metaExists:     false,
			probeError:     context.DeadlineExceeded,
			wantErrorIs:    ErrPreparing{},
			wantProbeCalls: 1,
		},
		{
			name:           "Heal Fail: Corrupt -> ErrUpstream",
			storeDuration:  50,
			resolveRootID:  "root1",
			resolvePath:    "rel/path",
			metaExists:     false,
			probeError:     vod.ErrProbeCorrupt,
			wantErrorIs:    ErrUpstream{},
			wantProbeCalls: 1,
		},

		// --- Fallback Removal ---
		{
			name:           "Probe Fail (No Defaults)",
			storeDuration:  0,
			resolveRootID:  "root1",
			resolvePath:    "rel/path",
			metaExists:     false,
			probeError:     vod.ErrProbeCorrupt,
			wantErrorIs:    ErrUpstream{}, // Must NOT return defaults
			wantProbeCalls: 1,
		},

		// --- Guardrail Cases ---
		{
			name:          "Store Get Error (No Overwrite)",
			storeDuration: 0,
			storeError:    errors.New("db fail"),
			resolveRootID: "root1",
			resolvePath:   "rel/path",
			metaExists:    false,
			probeResult: &vod.StreamInfo{
				Video:     vod.VideoStreamInfo{Duration: 100.0, CodecName: "h264"},
				Audio:     vod.AudioStreamInfo{CodecName: "aac"},
				Container: "mpegts",
			},
			wantDuration:      100,
			wantReason:        "probed_and_persisted",
			wantProbeCalls:    1,
			wantStoreSetCalls: 0, // Safety check
			wantMetaUpdates:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Mocks
			mockMeta := &mockMetadataManager{
				getMetadataFunc: func(ref string) (vod.Metadata, bool) {
					if !tt.metaExists {
						return vod.Metadata{}, false
					}
					return vod.Metadata{
						Duration:   tt.metaDuration,
						Container:  tt.metaContainer,
						VideoCodec: tt.metaVCodec,
						AudioCodec: tt.metaACodec,
						State:      vod.ArtifactStateReady,
					}, true
				},
				probeFunc: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
					return tt.probeResult, tt.probeError
				},
			}

			mockStore := &mockDurationStore{
				getDurationFunc: func(ctx context.Context, rootID, relPath string) (int64, bool, error) {
					if tt.storeError != nil {
						return 0, false, tt.storeError
					}
					if tt.storeDuration > 0 {
						return tt.storeDuration, true, nil
					}
					return 0, false, nil
				},
				setDurationFunc: func(ctx context.Context, rootID, relPath string, duration int64) error {
					return tt.storeSetError
				},
			}

			mockResolver := &mockPathResolver{
				resolveFunc: func(ref string) (string, string, string, error) {
					path := ""
					// Only return local path if Resolve expects valid one (simulate file existing)
					if tt.resolveRootID != "" {
						path = "/tmp/" + tt.resolvePath
					}
					return path, tt.resolveRootID, tt.resolvePath, nil
				},
			}

			// Initialize Service
			r := NewResolver(&config.AppConfig{
				HLS: config.HLSConfig{Root: "/tmp/hls_root"},
			}, mockMeta)
			r.WithDurationStore(mockStore, mockResolver)

			// Call Resolve
			ctx := context.Background()
			res, err := r.Resolve(ctx, "service:ref", "viewer", ProfileGeneric)

			// Assertions
			if tt.wantErrorIs != nil {
				require.Error(t, err)
				require.IsType(t, tt.wantErrorIs, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantDuration, res.MediaInfo.Duration)
			assert.Equal(t, tt.wantReason, res.Reason)

			// Side Effects
			assert.Equal(t, tt.wantProbeCalls, mockMeta.probeCalls, "Probe calls mismatch")
			assert.Equal(t, tt.wantStoreSetCalls, mockStore.setCalls, "Store Set calls mismatch")
			assert.Equal(t, tt.wantMetaUpdates, mockMeta.updateMetadataCalls, "Meta Update calls mismatch")
		})
	}
}

// TestMediaTruth_System_Heal verifies the "Heal" lifecycle strictly.
// 1. First call: Store Hit + Codec Miss -> Probe trigger -> Metadata Update (Heal)
// 2. Second call: Store Hit + Codec Hit -> No Probe (Stable)
func TestMediaTruth_System_Heal(t *testing.T) {
	// Stateful Mocks
	storeMap := map[string]int64{
		"root1/heal.ts": 120, // Store knows duration
	}

	// Metadata cache starts empty
	var cachedMeta vod.Metadata
	var cachedMetaExists bool

	mockStore := &mockDurationStore{
		getDurationFunc: func(ctx context.Context, rootID, relPath string) (int64, bool, error) {
			val, ok := storeMap[rootID+"/"+relPath]
			if ok {
				return val, true, nil
			}
			return 0, false, nil
		},
		setDurationFunc: func(ctx context.Context, rootID, relPath string, duration int64) error {
			return errors.New("should not set duration") // Store is read-only in heal scenario
		},
	}

	mockMeta := &mockMetadataManager{
		getMetadataFunc: func(ref string) (vod.Metadata, bool) {
			return cachedMeta, cachedMetaExists
		},
		updateMetadataFunc: func(serviceRef string, meta vod.Metadata) {
			cachedMeta = meta
			cachedMetaExists = true
		},
		probeFunc: func(ctx context.Context, path string) (*vod.StreamInfo, error) {
			return &vod.StreamInfo{
				Container: "mpegts",
				Video:     vod.VideoStreamInfo{Duration: 125.0, CodecName: "h264"}, // Probe duration distinct
				Audio:     vod.AudioStreamInfo{CodecName: "aac"},
			}, nil
		},
	}

	mockResolver := &mockPathResolver{
		resolveFunc: func(ref string) (string, string, string, error) {
			return "/tmp/heal.ts", "root1", "heal.ts", nil
		},
	}

	// Initialize
	r := NewResolver(&config.AppConfig{
		HLS: config.HLSConfig{Root: "/tmp/hls_root"},
	}, mockMeta)
	r.WithDurationStore(mockStore, mockResolver)

	ctx := context.Background()
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/heal.ts"

	// 1. Heal Call
	// Expect: Store Duration (120), But Probe called for codecs
	res1, err := r.Resolve(ctx, serviceRef, "viewer", ProfileGeneric)
	require.NoError(t, err)
	assert.Equal(t, 120.0, res1.MediaInfo.Duration)
	assert.Equal(t, "store_duration__probed_codecs", res1.Reason)
	assert.Equal(t, "h264", res1.MediaInfo.VideoCodec)
	assert.Equal(t, 1, mockMeta.probeCalls, "Should probe to heal codecs")

	// 2. Stable Call
	// Expect: Store Duration (120), Metadata Codecs (h264), No Probe
	res2, err := r.Resolve(ctx, serviceRef, "viewer", ProfileGeneric)
	require.NoError(t, err)
	assert.Equal(t, 120.0, res2.MediaInfo.Duration)
	assert.Equal(t, "store_duration__metadata_codecs", res2.Reason)
	assert.Equal(t, "h264", res2.MediaInfo.VideoCodec)
	assert.Equal(t, 1, mockMeta.probeCalls, "Should NOT probe again")
}
