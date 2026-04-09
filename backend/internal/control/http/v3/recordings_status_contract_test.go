package v3

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	recordingsmodel "github.com/ManuGH/xg2g/internal/domain/recordings/model"
)

func TestGetRecordings_StatusContractUsesCanonicalAPIStatuses(t *testing.T) {
	srv := NewServer(config.AppConfig{}, nil, nil)
	srv.SetDependencies(Dependencies{
		RecordingsService: stubHouseholdRecordingsService{
			listResult: recservice.ListResult{
				Recordings: []recservice.RecordingItem{
					{
						ServiceRef:  "1:0:1:AAAA",
						RecordingID: "scheduled-recording",
						Title:       "Scheduled Recording",
						Status:      recordingsmodel.RecordingStatusScheduled,
					},
					{
						ServiceRef:  "1:0:1:BBBB",
						RecordingID: "unknown-recording",
						Title:       "Unknown Recording",
						Status:      recordingsmodel.RecordingStatusUnknown,
					},
				},
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings", nil)
	rr := httptest.NewRecorder()

	srv.GetRecordings(rr, req, GetRecordingsParams{})

	require.Equal(t, http.StatusOK, rr.Code)

	var response RecordingResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&response))
	require.NotNil(t, response.Recordings)
	require.Len(t, *response.Recordings, 2)

	recordings := *response.Recordings
	require.Equal(t, RecordingItemStatusScheduled, recordings[0].Status)
	require.Equal(t, RecordingItemStatusUnknown, recordings[1].Status)
}
