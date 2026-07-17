package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	v3auth "github.com/ManuGH/xg2g/internal/control/http/v3/auth"
	v3intents "github.com/ManuGH/xg2g/internal/control/http/v3/intents"
	v3recordings "github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/domain/playbackplanner"
	"github.com/ManuGH/xg2g/internal/log"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createDivergenceTestServer(t *testing.T, scanner ChannelScanner) (*Server, *v3intents.PlanningHandoffStore) {
	t.Helper()
	cfg := config.AppConfig{}
	cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
	cfg.HLS.Root = "/tmp/hls"
	store := v3intents.NewPlanningHandoffStore(v3intents.PlanningHandoffStoreConfig{TTL: time.Minute})
	s := &Server{
		cfg:                    cfg,
		recordingsService:      new(MockRecordingsService),
		JWTSecret:              v3auth.TestSecret(),
		plannerReceiptStore:    store,
		plannerReceiptEnabled:  true,
		plannerReceiptRequired: true,
	}
	s.SetDependencies(Dependencies{Scan: scanner, RecordingsService: s.recordingsService})
	return s, store
}

func TestPlaybackInfoDivergenceMatrix_AllowInteractive(t *testing.T) {
	scanner := &fixedPlaybackInfoScanner{
		found: true,
		capability: scan.Capability{
			State:      scan.CapabilityStateOK,
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
			Width:      1920,
			Height:     1080,
			FPS:        25,
		},
	}
	s, _ := createDivergenceTestServer(t, scanner)

	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":3,
			"container":["hls","mpegts","ts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"],
			"supportsHls":true,
			"allowTranscode":true
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-interactive-allow-123"))

	s.PostLivePlaybackInfo(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	var info PlaybackInfo
	err := json.Unmarshal(w.Body.Bytes(), &info)
	require.NoError(t, err)

	assert.Equal(t, PlaybackInfoModeHls, info.Mode)
	require.NotNil(t, info.Url)
	assert.Equal(t, "/api/v3/streams/1:0:1:1234:5678:9ABC:0:0:0:0/playlist.m3u8", *info.Url)
	assert.Equal(t, "req-interactive-allow-123", info.RequestId)
	assert.Equal(t, "rec:1:0:1:1234:5678:9ABC:0:0:0:0", info.SessionId)
	require.NotNil(t, info.Decision)
	assert.Equal(t, "req-interactive-allow-123", info.Decision.Trace.RequestId)

	require.NotNil(t, info.PlaybackDecisionToken)
	claims, err := v3auth.VerifyStrict(*info.PlaybackDecisionToken, s.JWTSecret, "xg2g/v3/intents", "xg2g")
	require.NoError(t, err)
	assert.Equal(t, "req-interactive-allow-123", claims.TraceID)
	assert.NotEmpty(t, claims.ReceiptID)
	assert.NotEmpty(t, claims.EvidenceHash)
}

func TestPlaybackInfoDivergenceMatrix_AllowEpgBadge(t *testing.T) {
	scanner := &fixedPlaybackInfoScanner{
		found: true,
		capability: scan.Capability{
			State:      scan.CapabilityStateOK,
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
	}
	s, _ := createDivergenceTestServer(t, scanner)

	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":3,
			"container":["hls","mpegts","ts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"],
			"supportsHls":true,
			"allowTranscode":true
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(v3recordings.PlaybackInfoContextHeader, v3recordings.PlaybackInfoContextEpgBadge)
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-epg-badge-456"))

	s.PostLivePlaybackInfo(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	var info PlaybackInfo
	err := json.Unmarshal(w.Body.Bytes(), &info)
	require.NoError(t, err)

	assert.Equal(t, PlaybackInfoModeHls, info.Mode)
	require.NotNil(t, info.Url)
	assert.Equal(t, "/api/v3/streams/1:0:1:1234:5678:9ABC:0:0:0:0/playlist.m3u8", *info.Url)
	assert.Equal(t, "req-epg-badge-456", info.RequestId)
	require.NotNil(t, info.Decision)
	assert.Equal(t, "req-epg-badge-456", info.Decision.Trace.RequestId)

	// EPG badge MUST skip token issuance
	assert.Nil(t, info.PlaybackDecisionToken)
}

func TestPlaybackInfoDivergenceMatrix_Deny(t *testing.T) {
	scanner := &fixedPlaybackInfoScanner{
		found: true,
		capability: scan.Capability{
			State:      scan.CapabilityStateOK,
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
	}
	s, _ := createDivergenceTestServer(t, scanner)

	// Incompatible codecs and allowTranscode=false triggers policy_denies_transcode
	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":3,
			"container":["mp4"],
			"videoCodecs":["hevc"],
			"audioCodecs":["ac3"],
			"supportsHls":false,
			"allowTranscode":false
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(log.ContextWithRequestID(r.Context(), "req-deny-789"))

	s.PostLivePlaybackInfo(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	var info PlaybackInfo
	err := json.Unmarshal(w.Body.Bytes(), &info)
	require.NoError(t, err)

	assert.Equal(t, PlaybackInfoModeDeny, info.Mode)
	assert.Nil(t, info.Url)
	assert.Nil(t, info.PlaybackDecisionToken)
	assert.Equal(t, "req-deny-789", info.RequestId)
	assert.Equal(t, "rec:1:0:1:1234:5678:9ABC:0:0:0:0", info.SessionId)
	require.NotNil(t, info.Decision)
	assert.Equal(t, PlaybackDecisionModeDeny, info.Decision.Mode)
	assert.Empty(t, info.Decision.Outputs)
	assert.Equal(t, "req-deny-789", info.Decision.Trace.RequestId)
	assert.Contains(t, info.Decision.Reasons, string(playbackplanner.ReasonPolicyDeniesTranscode))
	require.NotNil(t, info.DecisionReason)
	assert.Equal(t, string(playbackplanner.ReasonPolicyDeniesTranscode), *info.DecisionReason)
	require.NotNil(t, info.Reason)
	assert.Equal(t, PlaybackInfoReasonUnknown, *info.Reason)
}

func TestPlaybackInfoDivergenceMatrix_RequestIDPropagation(t *testing.T) {
	scanner := &fixedPlaybackInfoScanner{
		found: true,
		capability: scan.Capability{
			State:      scan.CapabilityStateOK,
			Container:  "mpegts",
			VideoCodec: "h264",
			AudioCodec: "aac",
		},
	}
	s, _ := createDivergenceTestServer(t, scanner)

	// Test with omitted requestId -> verify auto-generated UUID propagated consistently across all three tiers
	body := `{
		"serviceRef":"1:0:1:1234:5678:9ABC:0:0:0:0:",
		"capabilities":{
			"capabilitiesVersion":3,
			"container":["hls","mpegts","ts"],
			"videoCodecs":["h264"],
			"audioCodecs":["aac"],
			"supportsHls":true,
			"allowTranscode":true
		}
	}`

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	s.PostLivePlaybackInfo(w, r)
	require.Equal(t, http.StatusOK, w.Code)

	var info PlaybackInfo
	err := json.Unmarshal(w.Body.Bytes(), &info)
	require.NoError(t, err)

	require.NotEmpty(t, info.RequestId)
	require.NotNil(t, info.Decision)
	assert.Equal(t, info.RequestId, info.Decision.Trace.RequestId)

	require.NotNil(t, info.PlaybackDecisionToken)
	claims, err := v3auth.VerifyStrict(*info.PlaybackDecisionToken, s.JWTSecret, "xg2g/v3/intents", "xg2g")
	require.NoError(t, err)
	assert.Equal(t, info.RequestId, claims.TraceID)
}
