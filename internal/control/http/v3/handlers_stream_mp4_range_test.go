package v3

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStreamRecordingDirect_RangeMatrix(t *testing.T) {
	// 1. Setup Deterministic Test Artifact
	tmpDir := t.TempDir()
	content := make([]byte, 4096)
	for i := range content {
		content[i] = byte(i % 256)
	}
	artifactPath := filepath.Join(tmpDir, "test.mp4")
	require.NoError(t, os.WriteFile(artifactPath, content, 0644))

	// size := int64(len(content)) // Not needed for some asserts, but good to have.

	// 2. Mock Service to return this path
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/movie.ts"
	recordingID := recservice.EncodeRecordingID(serviceRef)

	svc := new(MockRecordingsService)
	svc.On("Stream", mock.Anything, recservice.StreamInput{
		RecordingID: recordingID,
	}).Return(recservice.StreamResult{
		Ready:     true,
		LocalPath: artifactPath,
	}, nil)

	s := createTestServerDTO(svc)

	type testCase struct {
		name           string
		method         string
		rangeHeader    string
		wantStatus     int
		wantRange      string
		wantLen        int
		wantBodyPrefix []byte
	}

	tests := []testCase{
		{
			name:       "Full_GET",
			method:     "GET",
			wantStatus: http.StatusOK,
			wantLen:    4096,
		},
		{
			name:           "Partial_FirstByte",
			method:         "GET",
			rangeHeader:    "bytes=0-0",
			wantStatus:     http.StatusPartialContent,
			wantRange:      "bytes 0-0/4096",
			wantLen:        1,
			wantBodyPrefix: []byte{0x00},
		},
		{
			name:        "Partial_First100",
			method:      "GET",
			rangeHeader: "bytes=0-99",
			wantStatus:  http.StatusPartialContent,
			wantRange:   "bytes 0-99/4096",
			wantLen:     100,
		},
		{
			name:        "Partial_Suffix",
			method:      "GET",
			rangeHeader: "bytes=-100",
			wantStatus:  http.StatusPartialContent,
			wantRange:   "bytes 3996-4095/4096",
			wantLen:     100,
		},
		{
			name:        "Partial_OpenEnded",
			method:      "GET",
			rangeHeader: "bytes=4000-",
			wantStatus:  http.StatusPartialContent,
			wantRange:   "bytes 4000-4095/4096",
			wantLen:     96,
		},
		{
			name:        "Invalid_OutOfRange",
			method:      "GET",
			rangeHeader: "bytes=5000-",
			wantStatus:  http.StatusRequestedRangeNotSatisfiable,
			wantRange:   "bytes */4096",
		},
		{
			name:        "Invalid_MultiRange_PolicyA",
			method:      "GET",
			rangeHeader: "bytes=0-0,1-1",
			wantStatus:  http.StatusRequestedRangeNotSatisfiable,
			wantRange:   "bytes */4096",
		},
		{
			name:       "Full_HEAD",
			method:     "HEAD",
			wantStatus: http.StatusOK,
			wantLen:    4096,
		},
		{
			name:        "Partial_HEAD",
			method:      "HEAD",
			rangeHeader: "bytes=100-199",
			wantStatus:  http.StatusPartialContent,
			wantRange:   "bytes 100-199/4096",
			wantLen:     100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tt.method, "/api/v3/recordings/"+recordingID+"/stream.mp4", nil)
			if tt.rangeHeader != "" {
				r.Header.Set("Range", tt.rangeHeader)
			}

			if tt.method == "HEAD" {
				s.ProbeRecordingMp4(w, r, recordingID)
			} else {
				s.StreamRecordingDirect(w, r, recordingID)
			}

			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Equal(t, "video/mp4", w.Header().Get("Content-Type"))
			assert.Equal(t, "bytes", w.Header().Get("Accept-Ranges"))

			if tt.wantRange != "" {
				assert.Equal(t, tt.wantRange, w.Header().Get("Content-Range"))
			}

			if tt.wantStatus != http.StatusRequestedRangeNotSatisfiable {
				assert.Equal(t, fmt.Sprintf("%d", tt.wantLen), w.Header().Get("Content-Length"))
			}

			if tt.method == "HEAD" {
				assert.Empty(t, w.Body.Bytes())
			} else if tt.wantStatus == http.StatusPartialContent || tt.wantStatus == http.StatusOK {
				require.Equal(t, tt.wantLen, w.Body.Len())
				if len(tt.wantBodyPrefix) > 0 {
					assert.Equal(t, tt.wantBodyPrefix, w.Body.Bytes()[:len(tt.wantBodyPrefix)])
				}
			}
		})
	}
}
