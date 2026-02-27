package v3

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostLivePlaybackInfo_ModeFromCapabilities(t *testing.T) {
	serviceRef := "1:0:1:1111:2222:3333:0:0:0:0:"

	tests := []struct {
		name       string
		hlsEngines *[]PlaybackCapabilitiesHlsEngines
		wantMode   PlaybackInfoMode
	}{
		{
			name: "native_hls selected when native engine reported",
			hlsEngines: &[]PlaybackCapabilitiesHlsEngines{
				PlaybackCapabilitiesHlsEnginesNative,
			},
			wantMode: PlaybackInfoModeNativeHls,
		},
		{
			name: "hlsjs selected when hlsjs engine reported",
			hlsEngines: &[]PlaybackCapabilitiesHlsEngines{
				PlaybackCapabilitiesHlsEnginesHlsjs,
			},
			wantMode: PlaybackInfoModeHlsjs,
		},
		{
			name:       "old client payload without hlsEngines denies without fallback",
			hlsEngines: nil,
			wantMode:   PlaybackInfoModeDeny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.AppConfig{}
			cfg.FFmpeg.Bin = "/usr/bin/ffmpeg"
			cfg.HLS.Root = "/tmp/hls"
			s := &Server{
				cfg:                    cfg,
				liveDecisionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
			}

			reqBody := LivePlaybackInfoRequest{
				ServiceRef: serviceRef,
				Capabilities: PlaybackCapabilities{
					CapabilitiesVersion: 1,
					Container:           []string{"mp4", "ts", "mpegts"},
					VideoCodecs:         []string{"h264"},
					AudioCodecs:         []string{"aac"},
					SupportsHls:         boolPtr(true),
					HlsEngines:          tt.hlsEngines,
					SupportsRange:       boolPtr(true),
					AllowTranscode:      boolPtr(true),
				},
			}

			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", bytes.NewReader(body))
			s.PostLivePlaybackInfo(w, r)

			require.Equal(t, http.StatusOK, w.Code)

			var dto PlaybackInfo
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dto))
			assert.Equal(t, tt.wantMode, dto.Mode)
			require.NotNil(t, dto.Decision)

			if tt.wantMode == PlaybackInfoModeDeny {
				assert.Nil(t, dto.Decision.SelectedOutputUrl)
				assert.Nil(t, dto.Decision.SelectedOutputKind)
				assert.Nil(t, dto.PlaybackDecisionToken)
				assert.NotEqual(t, PlaybackInfoModeHlsjs, dto.Mode)
				assert.NotEqual(t, PlaybackInfoModeNativeHls, dto.Mode)
				return
			}

			assert.Equal(t, PlaybackDecisionMode("direct_stream"), dto.Decision.Mode)
			require.NotNil(t, dto.PlaybackDecisionToken)
			assert.NotEmpty(t, *dto.PlaybackDecisionToken)
		})
	}
}

func TestPostLivePlaybackInfo_DenyHasNoSelectedOutput(t *testing.T) {
	// No transcode capability on server + incompatible client codecs => deny.
	s := &Server{
		cfg:                    config.AppConfig{},
		liveDecisionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
	}

	hlsEngines := []PlaybackCapabilitiesHlsEngines{PlaybackCapabilitiesHlsEnginesHlsjs}
	reqBody := LivePlaybackInfoRequest{
		ServiceRef: "1:0:1:9999:8888:7777:0:0:0:0:",
		Capabilities: PlaybackCapabilities{
			CapabilitiesVersion: 1,
			Container:           []string{"mp4", "ts", "mpegts"},
			VideoCodecs:         []string{"vp9"},
			AudioCodecs:         []string{"aac"},
			SupportsHls:         boolPtr(true),
			HlsEngines:          &hlsEngines,
			SupportsRange:       boolPtr(true),
			AllowTranscode:      boolPtr(true),
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v3/live/stream-info", bytes.NewReader(body))
	s.PostLivePlaybackInfo(w, r)

	require.Equal(t, http.StatusOK, w.Code)

	var dto PlaybackInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &dto))
	assert.Equal(t, PlaybackInfoModeDeny, dto.Mode)
	assert.Nil(t, dto.Url)
	require.NotNil(t, dto.Decision)
	assert.Equal(t, PlaybackDecisionMode("deny"), dto.Decision.Mode)
	assert.Nil(t, dto.Decision.SelectedOutputUrl)
	assert.Nil(t, dto.Decision.SelectedOutputKind)
	assert.Nil(t, dto.PlaybackDecisionToken)
	assert.NotEmpty(t, dto.Decision.Reasons)
}
