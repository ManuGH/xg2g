package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/stretchr/testify/require"
)

func TestAlignScrubOffsetSeconds(t *testing.T) {
	f := func(v float32) *float32 { return &v }

	require.Equal(t, int64(0), alignScrubOffsetSeconds(nil))
	require.Equal(t, int64(0), alignScrubOffsetSeconds(f(-5)))
	require.Equal(t, int64(0), alignScrubOffsetSeconds(f(0)))
	require.Equal(t, int64(0), alignScrubOffsetSeconds(f(9.9)))
	require.Equal(t, int64(10), alignScrubOffsetSeconds(f(10)))
	require.Equal(t, int64(10), alignScrubOffsetSeconds(f(19.99)))
	require.Equal(t, int64(3600), alignScrubOffsetSeconds(f(3605.4)))
	require.Equal(t, int64(0), alignScrubOffsetSeconds(f(float32(0)/1))) // sanity
}

func TestGetRecordingScrubFrame_NotFoundWhenUnmapped(t *testing.T) {
	server := NewServer(config.AppConfig{}, nil, nil)
	server.SetDependencies(Dependencies{})

	recordingID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/missing.ts")
	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/scrub.jpg?t=30", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"})))

	rr := httptest.NewRecorder()
	server.GetRecordingScrubFrame(rr, req, recordingID, GetRecordingScrubFrameParams{})

	// No path mapper configured: the source cannot be resolved locally.
	require.Equal(t, http.StatusNotFound, rr.Code)
}
