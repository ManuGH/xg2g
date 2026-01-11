package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

func TestMapRecordingStatus(t *testing.T) {
	cases := []struct {
		name   string
		job    *vod.JobStatus
		jobOk  bool
		meta   *vod.Metadata
		metaOk bool
		state  RecordingBuildStatusState
		err    string
	}{
		{
			name:  "job running",
			job:   &vod.JobStatus{State: vod.JobStateBuilding},
			jobOk: true,
			state: RecordingBuildStatusStateRUNNING,
		},
		{
			name:  "job failed",
			job:   &vod.JobStatus{State: vod.JobStateFailed, Reason: "CRASH"},
			jobOk: true,
			state: RecordingBuildStatusStateFAILED,
			err:   "CRASH",
		},
		{
			name:  "job succeeded",
			job:   &vod.JobStatus{State: vod.JobStateSucceeded},
			jobOk: true,
			state: RecordingBuildStatusStateREADY,
		},
		{
			name:   "meta unknown",
			meta:   &vod.Metadata{State: vod.ArtifactStateUnknown},
			metaOk: true,
			state:  RecordingBuildStatusStateRUNNING,
		},
		{
			name:   "meta preparing",
			meta:   &vod.Metadata{State: vod.ArtifactStatePreparing},
			metaOk: true,
			state:  RecordingBuildStatusStateRUNNING,
		},
		{
			name:   "meta ready",
			meta:   &vod.Metadata{State: vod.ArtifactStateReady},
			metaOk: true,
			state:  RecordingBuildStatusStateREADY,
		},
		{
			name:   "meta failed",
			meta:   &vod.Metadata{State: vod.ArtifactStateFailed, Error: "oops"},
			metaOk: true,
			state:  RecordingBuildStatusStateFAILED,
			err:    "oops",
		},
		{
			name:   "meta missing",
			meta:   &vod.Metadata{State: vod.ArtifactStateMissing},
			metaOk: true,
			state:  RecordingBuildStatusStateFAILED,
			err:    "MISSING",
		},
		{
			name:   "no state",
			meta:   nil,
			metaOk: false,
			state:  RecordingBuildStatusStateIDLE,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := mapRecordingStatus(c.job, c.jobOk, c.meta, c.metaOk)
			require.Equal(t, c.state, resp.State)
			if c.err == "" {
				require.Nil(t, resp.Error)
			} else {
				require.NotNil(t, resp.Error)
				require.Equal(t, c.err, *resp.Error)
			}
		})
	}
}

func TestRecordingsStatus_NoSyncFSCalls(t *testing.T) {
	forbidden := []string{
		"RecordingPlaylistReady", "RecordingLivePlaylistReady",
		"os.Stat", "os.ReadFile", "os.ReadDir", "os.Open", "os.Lstat",
		"filepath.Glob", "fs.ConfineRelPath",
	}

	content, err := os.ReadFile("recordings.go")
	require.NoError(t, err)

	lines := strings.Split(string(content), "\n")
	inHandler := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			inHandler = strings.HasPrefix(trimmed, "func (s *Server) GetRecordingsRecordingIdStatus")
		}
		if inHandler {
			for _, f := range forbidden {
				if strings.Contains(line, f) && !strings.Contains(line, "//") {
					t.Errorf("Forbidden call %q found in status handler at recordings.go:%d", f, i+1)
				}
			}
		}
	}
}

func TestRecordingStatus_ErrorOmittedWhenNil(t *testing.T) {
	resp := mapRecordingStatus(nil, false, nil, false)
	require.Equal(t, RecordingBuildStatusStateIDLE, resp.State)
	require.Nil(t, resp.Error)
}

func TestRecordingsStatusHTTP_IdleOmitsError(t *testing.T) {
	srv, _ := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := EncodeRecordingID(serviceRef)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, "IDLE", body["state"])
	_, hasError := body["error"]
	require.False(t, hasError)
}

func TestRecordingsStatusHTTP_MetaFailedIncludesError(t *testing.T) {
	srv, vodMgr := newStatusTestServer(t)
	serviceRef := "1:0:0:0:0:0:0:0:0:/media/test.ts"
	recordingID := EncodeRecordingID(serviceRef)
	vodMgr.UpdateMetadata(serviceRef, vod.Metadata{
		State: vod.ArtifactStateFailed,
		Error: "oops",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/status", nil)
	rr := httptest.NewRecorder()
	srv.GetRecordingsRecordingIdStatus(rr, req, recordingID)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")

	var body map[string]any
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&body))
	require.Equal(t, "FAILED", body["state"])
	require.Equal(t, "oops", body["error"])
}

func newStatusTestServer(t *testing.T) (*Server, *vod.Manager) {
	t.Helper()
	cfg := config.AppConfig{
		HLS: config.HLSConfig{
			Root: t.TempDir(),
		},
	}
	srv := NewServer(cfg, nil, nil)
	vodMgr := vod.NewManager(nil, nil, nil) // Wire dependencies
	srv.SetDependencies(
		nil, nil, nil, nil, nil, nil, nil, nil, vodMgr, nil, // P3: VODResolver (nil)
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	return srv, vodMgr
}
