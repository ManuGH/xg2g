package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/clientplayback"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockRecSvc2 struct{ mock.Mock }

func (m *mockRecSvc2) ResolvePlayback(ctx context.Context, recordingID, profile string) (recservice.PlaybackResolution, error) {
	args := m.Called(ctx, recordingID, profile)
	return args.Get(0).(recservice.PlaybackResolution), args.Error(1)
}
func (m *mockRecSvc2) List(ctx context.Context, in recservice.ListInput) (recservice.ListResult, error) {
	return recservice.ListResult{}, nil
}
func (m *mockRecSvc2) GetPlaybackInfo(ctx context.Context, in recservice.PlaybackInfoInput) (recservice.PlaybackInfoResult, error) {
	return recservice.PlaybackInfoResult{}, nil
}
func (m *mockRecSvc2) GetStatus(ctx context.Context, in recservice.StatusInput) (recservice.StatusResult, error) {
	return recservice.StatusResult{}, nil
}
func (m *mockRecSvc2) Stream(ctx context.Context, in recservice.StreamInput) (recservice.StreamResult, error) {
	return recservice.StreamResult{}, nil
}
func (m *mockRecSvc2) Delete(ctx context.Context, in recservice.DeleteInput) (recservice.DeleteResult, error) {
	return recservice.DeleteResult{}, nil
}

func TestClientPlaybackInfo_StrictFailClosed(t *testing.T) {
	t.Run("UnknownCodecs => Transcode", func(t *testing.T) {
		svc := new(mockRecSvc2)
		svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(recservice.PlaybackResolution{
			Strategy:   recservice.StrategyDirect,
			CanSeek:    true,
			Container:  nil,
			VideoCodec: nil,
			AudioCodec: nil,
		}, nil)

		req := clientplayback.PlaybackInfoRequest{
			DeviceProfile: &clientplayback.DeviceProfile{
				DirectPlayProfiles: []clientplayback.DirectPlayProfile{
					{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
				},
			},
		}
		body, _ := json.Marshal(req)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/Items/rec1/PlaybackInfo", bytes.NewReader(body))

		s.PostItemsPlaybackInfo(w, r, "rec1")
		assert.Equal(t, http.StatusOK, w.Code)

		var out clientplayback.PlaybackInfoResponse
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Len(t, out.MediaSources, 1)
		assert.False(t, out.MediaSources[0].SupportsDirectPlay)
		assert.NotNil(t, out.MediaSources[0].TranscodingUrl)
		assert.Equal(t, "/api/v3/recordings/rec1/playlist.m3u8", *out.MediaSources[0].TranscodingUrl)
	})

	t.Run("Known+Supported => DirectPlay", func(t *testing.T) {
		svc := new(mockRecSvc2)
		c := "mp4"
		v := "h264"
		a := "aac"
		dur := int64(60)
		svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(recservice.PlaybackResolution{
			Strategy:    recservice.StrategyDirect,
			CanSeek:     true,
			DurationSec: &dur,
			Container:   &c,
			VideoCodec:  &v,
			AudioCodec:  &a,
		}, nil)

		req := clientplayback.PlaybackInfoRequest{
			DeviceProfile: &clientplayback.DeviceProfile{
				DirectPlayProfiles: []clientplayback.DirectPlayProfile{
					{Type: "Video", Container: "mp4,m4v", VideoCodec: "h264", AudioCodec: "aac,ac3"},
				},
			},
		}
		body, _ := json.Marshal(req)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/Items/rec1/PlaybackInfo", bytes.NewReader(body))

		s.PostItemsPlaybackInfo(w, r, "rec1")
		assert.Equal(t, http.StatusOK, w.Code)

		var out clientplayback.PlaybackInfoResponse
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Len(t, out.MediaSources, 1)
		ms := out.MediaSources[0]
		assert.True(t, ms.SupportsDirectPlay)
		assert.Equal(t, "/api/v3/recordings/rec1/stream.mp4", ms.Path)
		assert.NotNil(t, ms.Container)
		assert.Equal(t, "mp4", *ms.Container)
		assert.Nil(t, ms.TranscodingUrl)
		assert.NotNil(t, ms.RunTimeTicks)
		assert.Equal(t, int64(60*10_000_000), *ms.RunTimeTicks)
	})

	t.Run("Known+Unsupported => Transcode", func(t *testing.T) {
		svc := new(mockRecSvc2)
		c := "mp4"
		v := "hevc"
		a := "aac"
		svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(recservice.PlaybackResolution{
			Strategy:   recservice.StrategyDirect,
			CanSeek:    true,
			Container:  &c,
			VideoCodec: &v,
			AudioCodec: &a,
		}, nil)

		req := clientplayback.PlaybackInfoRequest{
			DeviceProfile: &clientplayback.DeviceProfile{
				DirectPlayProfiles: []clientplayback.DirectPlayProfile{
					{Type: "Video", Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
				},
			},
		}
		body, _ := json.Marshal(req)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/Items/rec1/PlaybackInfo", bytes.NewReader(body))

		s.PostItemsPlaybackInfo(w, r, "rec1")
		assert.Equal(t, http.StatusOK, w.Code)

		var out clientplayback.PlaybackInfoResponse
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		assert.Len(t, out.MediaSources, 1)
		assert.False(t, out.MediaSources[0].SupportsDirectPlay)
		assert.NotNil(t, out.MediaSources[0].TranscodingUrl)
	})

	t.Run("DriftGuard_JsonKeys", func(t *testing.T) {
		svc := new(mockRecSvc2)
		dur := int64(10) // 10 seconds
		svc.On("ResolvePlayback", mock.Anything, "rec1", "generic").Return(recservice.PlaybackResolution{
			Strategy:    recservice.StrategyDirect,
			DurationSec: &dur,
		}, nil)

		// No device profile -> transcode default
		req := clientplayback.PlaybackInfoRequest{}
		body, _ := json.Marshal(req)

		s := &Server{recordingsService: svc}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/Items/rec1/PlaybackInfo", bytes.NewReader(body))

		s.PostItemsPlaybackInfo(w, r, "rec1")
		assert.Equal(t, http.StatusOK, w.Code)

		// Unmarshal to generic map to check keys
		var raw map[string]interface{}
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

		// Check root keys
		assert.Contains(t, raw, "MediaSources")

		// Check MediaSource keys
		sources := raw["MediaSources"].([]interface{})
		assert.Len(t, sources, 1)
		ms := sources[0].(map[string]interface{})

		assert.Contains(t, ms, "Path")
		assert.Contains(t, ms, "Protocol")
		assert.Contains(t, ms, "SupportsDirectPlay")
		assert.Contains(t, ms, "SupportsDirectStream")
		assert.Contains(t, ms, "SupportsTranscoding")

		// Verify Ticks Conversion (10s * 10M)
		// JSON numbers often float64 in generic map
		ticks := ms["RunTimeTicks"].(float64)
		assert.Equal(t, float64(100_000_000), ticks)
	})
}
