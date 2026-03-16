package v3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	admissionmonitor "github.com/ManuGH/xg2g/internal/admission"
	"github.com/ManuGH/xg2g/internal/config"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/control/playback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/hardware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockRecordingsService for testing handler interaction
type MockRecordingsService struct {
	mock.Mock
}

func (m *MockRecordingsService) ResolvePlayback(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error) {
	args := m.Called(ctx, recordingID, profile)
	return args.Get(0).(recservice.PlaybackResolution), args.Error(1)
}
func (m *MockRecordingsService) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	return recservice.ListResult{}, nil
}
func (m *MockRecordingsService) GetPlaybackInfo(ctx context.Context, in recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}
func (m *MockRecordingsService) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	return recservice.StatusResult{}, nil
}
func (m *MockRecordingsService) Stream(ctx context.Context, in recservice.StreamInput) (recservice.StreamResult, error) {
	args := m.Called(ctx, in)
	return args.Get(0).(recservice.StreamResult), args.Error(1)
}
func (m *MockRecordingsService) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}
func (m *MockRecordingsService) GetMediaTruth(ctx context.Context, recordingID string) (playback.MediaTruth, error) {
	args := m.Called(ctx, recordingID)
	return args.Get(0).(playback.MediaTruth), args.Error(1)
}

func createTestServerDTO(svc recservice.Service) *Server {
	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"
	s := &Server{cfg: cfg, recordingsService: svc}
	return s
}

func requireVariantAwareRecordingURL(t *testing.T, rawURL, recordingID string) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	assert.Equal(t, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", parsed.Path)

	query := parsed.Query()
	assert.Equal(t, "generic", query.Get("profile"))
	assert.NotEmpty(t, query.Get("variant"))
	assert.NotEmpty(t, query.Get("target"))

	target, err := v3recordings.DecodeTargetProfileQuery(query.Get("target"))
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, query.Get("variant"), v3recordings.TargetVariantHash(target))
}

func TestGetRecordingPlaybackInfo_StrictTruthfulness(t *testing.T) {
	// 1. Matrix: Error Codes
	tests := []struct {
		name       string
		mockErr    error
		wantStatus int
		wantBody   string
		wantHeader map[string]string
	}{
		{
			name:       "NotFound",
			mockErr:    recservice.ErrNotFound{RecordingID: "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "Preparing",
			mockErr:    recservice.ErrPreparing{RecordingID: "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"},
			wantStatus: http.StatusServiceUnavailable,
			wantHeader: map[string]string{"Retry-After": "5"},
		},
		{
			name:       "InvalidArgument",
			mockErr:    recservice.ErrInvalidArgument{Field: "id", Reason: "bad"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "UpstreamError",
			mockErr:    recservice.ErrUpstream{Op: "probe", Cause: errors.New("timeout")},
			wantStatus: http.StatusBadGateway,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
			recordingID := recservice.EncodeRecordingID(serviceRef)

			svc := new(MockRecordingsService)
			svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{}, tt.mockErr)

			s := createTestServerDTO(svc)
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

			s.GetRecordingPlaybackInfo(w, r, recordingID)

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantHeader != nil {
				for k, v := range tt.wantHeader {
					assert.Equal(t, v, w.Header().Get(k))
				}
			}
		})
	}

	// 2. Strict DTO Mapping (Nil Semantics)
	type testPlaybackInfoDTO struct {
		Mode            PlaybackInfoMode            `json:"mode"`
		Url             *string                     `json:"url"`
		Seekable        *bool                       `json:"seekable,omitempty"`
		DurationSeconds *int64                      `json:"durationSeconds,omitempty"`
		DurationSource  *PlaybackInfoDurationSource `json:"duration_source,omitempty"`
		RequestId       string                      `json:"requestId"`
		SessionId       string                      `json:"sessionId"`

		Container  *string `json:"container,omitempty"`
		VideoCodec *string `json:"videoCodec,omitempty"`
		AudioCodec *string `json:"audioCodec,omitempty"`
	}

	t.Run("DTO_Mapping_UnknownDuration_UnknownCodecs", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

		svc := new(MockRecordingsService)
		// Decision engine now returns 422 if codecs are unknown/ambiguous
		svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{
			Container: "", VideoCodec: "", AudioCodec: "",
		}, nil)

		s := createTestServerDTO(svc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("DTO_Mapping_KnownDuration_KnownCodecs", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec1.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

		svc := new(MockRecordingsService)
		truth := playback.MediaTruth{
			Duration:   3600,
			Container:  "ts",
			VideoCodec: "vp9", // Force transcode on web_conservative
			AudioCodec: "aac",
		}
		svc.On("GetMediaTruth", mock.Anything, recordingID).Return(truth, nil)

		s := createTestServerDTO(svc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
		r = r.WithContext(log.ContextWithRequestID(r.Context(), "test-req-123"))

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)
		var dto testPlaybackInfoDTO
		err := json.Unmarshal(w.Body.Bytes(), &dto)
		assert.NoError(t, err)

		// Expect HLS because VP9 is not in web_conservative (H264)
		assert.Equal(t, PlaybackInfoModeHls, dto.Mode)
		require.NotNil(t, dto.Url)
		requireVariantAwareRecordingURL(t, *dto.Url, recordingID)
		assert.NotEmpty(t, dto.RequestId)
		assert.NotEmpty(t, dto.SessionId)
	})

	t.Run("DTO_Mapping_ExposesExplicitVideoLadderForVideoTranscode", func(t *testing.T) {
		serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/rec-video-ladder.ts"
		recordingID := recservice.EncodeRecordingID(serviceRef)

		svc := new(MockRecordingsService)
		svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{
			Duration:   1800,
			Container:  "mp4",
			VideoCodec: "vp9",
			AudioCodec: "aac",
		}, nil)

		s := createTestServerDTO(svc)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)

		s.GetRecordingPlaybackInfo(w, r, recordingID)

		require.Equal(t, http.StatusOK, w.Code)

		var raw map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &raw)
		require.NoError(t, err)

		dec, ok := raw["decision"].(map[string]any)
		require.True(t, ok)
		trace, ok := dec["trace"].(map[string]any)
		require.True(t, ok)
		target, ok := trace["targetProfile"].(map[string]any)
		require.True(t, ok)
		video, ok := target["video"].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "compatible_video_h264_crf23_fast", trace["qualityRung"])
		assert.Equal(t, "compatible_video_h264_crf23_fast", trace["videoQualityRung"])
		assert.Nil(t, trace["audioQualityRung"])
		assert.Equal(t, float64(23), video["crf"])
		assert.Equal(t, "fast", video["preset"])
	})
}

// Regression Test: ID Ownership
func TestGetRecordingPlaybackInfo_ID_Ownership_StrictHexRequirement(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/fail.ts"
	recordingID_Hex := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)

	svc.On("GetMediaTruth", mock.Anything, recordingID_Hex).Return(playback.MediaTruth{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil).Once()

	s := createTestServerDTO(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID_Hex+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID_Hex)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/api/v3/recordings/"+serviceRef+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, serviceRef)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetRecordingPlaybackInfo_Deny_OptionB(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/deny.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)
	// Force a Deny decision via policy: AllowTranscode=false, Input needs transcode (flv)
	truth := playback.MediaTruth{
		Container:  "flv",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}
	svc.On("GetMediaTruth", mock.Anything, recordingID).Return(truth, nil)

	// We use the real server to exercise the mapPlaybackInfoV2 logic
	cfg := config.AppConfig{}
	// FFmpeg bin empty -> serverCanTranscode = false -> allowTranscode = false
	s := &Server{cfg: cfg, recordingsService: svc}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	assert.NoError(t, err)

	// 1. Mode uses HLS when DirectStream is selected
	assert.Equal(t, "hls", raw["mode"])
	// 2. URL points at HLS playlist
	requireVariantAwareRecordingURL(t, raw["url"].(string), recordingID)

	// 3. Decision sub-object is TRUTHFUL
	dec, ok := raw["decision"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "direct_stream", dec["mode"])
	assert.Equal(t, "hls", dec["selectedOutputKind"])
	assert.NotEmpty(t, dec["outputs"])

	trace, ok := dec["trace"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "compatible", trace["requestProfile"])
	assert.NotEmpty(t, trace["targetProfileHash"])
	assert.Equal(t, "compatible", trace["resolvedIntent"])
	assert.Equal(t, "compatible_hls_ts", trace["qualityRung"])
}

func TestGetRecordingPlaybackInfo_OperatorForceIntentThreadsIntoDecision(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/force-repair.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil)

	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"
	cfg.Playback.Operator.ForceIntent = "repair"
	s := &Server{cfg: cfg, recordingsService: svc}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	dec, ok := raw["decision"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "transcode", dec["mode"])
}

func TestGetRecordingPlaybackInfo_PerSourceOperatorForceIntentThreadsIntoDecision(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/source-force-repair.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "aac",
	}, nil)

	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"
	cfg.Playback.Operator.SourceRules = []config.PlaybackOperatorRuleConfig{
		{
			Name:        "problem-recording-source",
			Mode:        "recording",
			ServiceRef:  serviceRef,
			ForceIntent: "repair",
		},
	}
	s := &Server{cfg: cfg, recordingsService: svc}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	dec, ok := raw["decision"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "transcode", dec["mode"])

	trace, ok := dec["trace"].(map[string]any)
	require.True(t, ok)
	operator, ok := trace["operator"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "repair", operator["forcedIntent"])
	assert.Equal(t, "problem-recording-source", operator["ruleName"])
	assert.Equal(t, "recording", operator["ruleScope"])
	assert.Equal(t, true, operator["overrideApplied"])
}

func TestGetRecordingPlaybackInfo_HostPressureThreadsIntoDecisionTrace(t *testing.T) {
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/hdd/movie/host-pressure.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)
	svc.On("GetMediaTruth", mock.Anything, recordingID).Return(playback.MediaTruth{
		Container:  "mp4",
		VideoCodec: "h264",
		AudioCodec: "ac3",
	}, nil)

	s := createTestServerDTO(svc)
	s.admissionState = &MockAdmissionState{Tuners: 2, Sessions: 0, Transcodes: 0}
	s.hostPressureMonitor = admissionmonitor.NewResourceMonitor(8, 2, 1.5)
	s.hostPressureTracker = hardware.NewPressureTracker()
	for i := 0; i < 15; i++ {
		s.hostPressureMonitor.ObserveCPULoad(999)
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v3/recordings/"+recordingID+"/stream-info?profile=quality", nil)
	s.GetRecordingPlaybackInfo(w, r, recordingID)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	dec, ok := raw["decision"].(map[string]any)
	require.True(t, ok)
	trace, ok := dec["trace"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "critical", trace["hostPressureBand"])
	assert.Equal(t, true, trace["hostOverrideApplied"])
	assert.Equal(t, "quality", trace["degradedFrom"])
	assert.Equal(t, "compatible", trace["resolvedIntent"])
}

func TestPostLivePlaybackInfo_ValidServiceRef_AcceptsLiveRef(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mpegts","ts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"],
			"hlsEngines":["native"],
			"preferredHlsEngine":"native",
			"runtimeProbeUsed":true,
			"runtimeProbeVersion":1,
			"clientFamilyFallback":"safari_native"
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	s.PostLivePlaybackInfo(w, r)

	assert.NotEqual(t, http.StatusBadRequest, w.Code, "valid live serviceRef should not fail as invalid recording id")
	assert.NotContains(t, w.Body.String(), "Invalid recording ID format")
}

func TestPostLivePlaybackInfo_RuntimeProbeThreadsClientCapabilityTrace(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mp4","ts"],
			"videoCodecs":["hevc","h264"],
			"audioCodecs":["aac","mp3","ac3"],
			"supportsHls":true,
			"supportsRange":true,
			"deviceType":"web",
			"hlsEngines":["native"],
			"preferredHlsEngine":"native",
			"runtimeProbeUsed":true,
			"runtimeProbeVersion":1,
			"clientFamilyFallback":"safari_native"
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info?profile=quality", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	s.PostLivePlaybackInfo(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	dec, ok := raw["decision"].(map[string]any)
	require.True(t, ok)
	trace, ok := dec["trace"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "runtime_plus_family", trace["clientCapsSource"])
	assert.Equal(t, "safari_native", trace["clientFamily"])
}

func TestPostLivePlaybackInfo_FamilyFallbackOnlyThreadsCapabilityTrace(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":2,
			"container":["mp4","ts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac","mp3"],
			"deviceType":"web",
			"clientFamilyFallback":"ios_safari_native"
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	s.PostLivePlaybackInfo(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var raw map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &raw)
	require.NoError(t, err)

	dec, ok := raw["decision"].(map[string]any)
	require.True(t, ok)
	trace, ok := dec["trace"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "family_fallback", trace["clientCapsSource"])
	assert.Equal(t, "ios_safari_native", trace["clientFamily"])
}

func TestPostLivePlaybackInfo_InvalidServiceRef_RejectsNonLiveFormat(t *testing.T) {
	svc := new(MockRecordingsService)
	s := createTestServerDTO(svc)

	body := `{
		"serviceRef":"channel_abc",
		"capabilities":{
			"capabilitiesVersion":1,
			"container":["mpegts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"]
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	s.PostLivePlaybackInfo(w, r)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "serviceRef must be a valid live Enigma2 reference")
}
