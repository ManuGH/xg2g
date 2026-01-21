package v3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockArtifactResolver
type MockArtifactResolver struct {
	mock.Mock
}

func (m *MockArtifactResolver) ResolvePlaylist(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, profile)
	err, _ := args.Get(1).(*artifacts.ArtifactError)
	return args.Get(0).(artifacts.ArtifactOK), err
}

func (m *MockArtifactResolver) ResolveTimeshift(ctx context.Context, recordingID, profile string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, profile)
	err, _ := args.Get(1).(*artifacts.ArtifactError)
	return args.Get(0).(artifacts.ArtifactOK), err
}

func (m *MockArtifactResolver) ResolveSegment(ctx context.Context, recordingID string, segment string) (artifacts.ArtifactOK, *artifacts.ArtifactError) {
	args := m.Called(ctx, recordingID, segment)
	err, _ := args.Get(1).(*artifacts.ArtifactError)
	return args.Get(0).(artifacts.ArtifactOK), err
}

func TestHLS_ProfilePropagation(t *testing.T) {
	tests := []struct {
		name            string
		userAgent       string
		expectedProfile string
	}{
		{
			name:            "Safari_Mac",
			userAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Safari/605.1.15",
			expectedProfile: "safari",
		},
		{
			name:            "Generic_Chrome",
			userAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
			expectedProfile: "generic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := new(MockArtifactResolver)
			svc.On("ResolvePlaylist", mock.Anything, "rec1", tt.expectedProfile).Return(artifacts.ArtifactOK{Data: []byte("ok"), Kind: artifacts.ArtifactKindPlaylist}, (*artifacts.ArtifactError)(nil))

			s := &Server{artifacts: svc}
			w := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/api/v3/recordings/rec1/playlist.m3u8", nil)
			r.Header.Set("User-Agent", tt.userAgent)

			s.GetRecordingHLSPlaylist(w, r, "rec1")

			assert.Equal(t, http.StatusOK, w.Code)
			svc.AssertExpectations(t)
		})
	}
}

func TestHLSHandlers_Matrix(t *testing.T) {
	tmpDir := t.TempDir()
	segContent := make([]byte, 1024)
	for i := range segContent {
		segContent[i] = byte(i % 256)
	}
	segPath := filepath.Join(tmpDir, "seg_0.ts")
	require.NoError(t, os.WriteFile(segPath, segContent, 0644))

	recordingID := "test-rec"
	now := time.Now().Truncate(time.Second)

	mockRes := new(MockArtifactResolver)
	s := &Server{
		artifacts: mockRes,
	}

	tests := []struct {
		name        string
		target      string // playlist, segment
		method      string
		rangeHeader string
		setupMock   func()
		wantStatus  int
		wantType    string
		wantRange   string
		wantLen     int
	}{
		{
			name:   "Playlist_200_GET",
			target: "playlist",
			method: "GET",
			setupMock: func() {
				mockRes.On("ResolvePlaylist", mock.Anything, recordingID, mock.Anything).Return(artifacts.ArtifactOK{
					Data:    []byte("#EXTM3U\n"),
					ModTime: now,
					Kind:    artifacts.ArtifactKindPlaylist,
				}, (*artifacts.ArtifactError)(nil)).Once()
			},
			wantStatus: http.StatusOK,
			wantType:   "application/vnd.apple.mpegurl",
			wantLen:    8,
		},
		{
			name:   "Playlist_200_HEAD",
			target: "playlist",
			method: "HEAD",
			setupMock: func() {
				mockRes.On("ResolvePlaylist", mock.Anything, recordingID, mock.Anything).Return(artifacts.ArtifactOK{
					Data:    []byte("#EXTM3U\n"),
					ModTime: now,
					Kind:    artifacts.ArtifactKindPlaylist,
				}, (*artifacts.ArtifactError)(nil)).Once()
			},
			wantStatus: http.StatusOK,
			wantType:   "application/vnd.apple.mpegurl",
			wantLen:    0,
		},
		{
			name:        "Playlist_416_RangeViolation",
			target:      "playlist",
			method:      "GET",
			rangeHeader: "bytes=0-0",
			setupMock: func() {
				mockRes.On("ResolvePlaylist", mock.Anything, recordingID, mock.Anything).Return(artifacts.ArtifactOK{
					Data:    []byte("#EXTM3U\n"),
					ModTime: now,
					Kind:    artifacts.ArtifactKindPlaylist,
				}, (*artifacts.ArtifactError)(nil)).Once()
			},
			wantStatus: http.StatusRequestedRangeNotSatisfiable,
			wantRange:  "bytes */8",
		},
		{
			name:   "Playlist_503_Preparing",
			target: "playlist",
			method: "GET",
			setupMock: func() {
				mockRes.On("ResolvePlaylist", mock.Anything, recordingID, mock.Anything).Return(artifacts.ArtifactOK{}, &artifacts.ArtifactError{
					Code:       artifacts.CodePreparing,
					RetryAfter: 5 * time.Second,
				}).Once()
			},
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:   "Segment_200_GET",
			target: "segment",
			method: "GET",
			setupMock: func() {
				mockRes.On("ResolveSegment", mock.Anything, recordingID, "seg_0.ts").Return(artifacts.ArtifactOK{
					AbsPath: segPath,
					ModTime: now,
					Kind:    artifacts.ArtifactKindSegmentTS,
				}, (*artifacts.ArtifactError)(nil)).Once()
			},
			wantStatus: http.StatusOK,
			wantType:   "video/mp2t",
			wantLen:    1024,
		},
		{
			name:        "Segment_206_Range",
			target:      "segment",
			method:      "GET",
			rangeHeader: "bytes=0-99",
			setupMock: func() {
				mockRes.On("ResolveSegment", mock.Anything, recordingID, "seg_0.ts").Return(artifacts.ArtifactOK{
					AbsPath: segPath,
					ModTime: now,
					Kind:    artifacts.ArtifactKindSegmentTS,
				}, (*artifacts.ArtifactError)(nil)).Once()
			},
			wantStatus: http.StatusPartialContent,
			wantType:   "video/mp2t",
			wantRange:  "bytes 0-99/1024",
			wantLen:    100,
		},
		{
			name:        "Segment_416_InvalidRange",
			target:      "segment",
			method:      "GET",
			rangeHeader: "bytes=2000-",
			setupMock: func() {
				mockRes.On("ResolveSegment", mock.Anything, recordingID, "seg_0.ts").Return(artifacts.ArtifactOK{
					AbsPath: segPath,
					ModTime: now,
					Kind:    artifacts.ArtifactKindSegmentTS,
				}, (*artifacts.ArtifactError)(nil)).Once()
			},
			wantStatus: http.StatusRequestedRangeNotSatisfiable,
			wantRange:  "bytes */1024",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock()
			w := httptest.NewRecorder()
			var r *http.Request
			if tt.target == "playlist" {
				r = httptest.NewRequest(tt.method, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
				if tt.rangeHeader != "" {
					r.Header.Set("Range", tt.rangeHeader)
				}
				if tt.method == "HEAD" {
					s.GetRecordingHLSPlaylistHead(w, r, recordingID)
				} else {
					s.GetRecordingHLSPlaylist(w, r, recordingID)
				}
			} else {
				r = httptest.NewRequest(tt.method, "/api/v3/recordings/"+recordingID+"/seg_0.ts", nil)
				if tt.rangeHeader != "" {
					r.Header.Set("Range", tt.rangeHeader)
				}
				if tt.method == "HEAD" {
					s.GetRecordingHLSCustomSegmentHead(w, r, recordingID, "seg_0.ts")
				} else {
					s.GetRecordingHLSCustomSegment(w, r, recordingID, "seg_0.ts")
				}
			}

			assert.Equal(t, tt.wantStatus, w.Code)
			if tt.wantStatus == http.StatusOK || tt.wantStatus == http.StatusPartialContent {
				assert.Equal(t, tt.wantType, w.Header().Get("Content-Type"))
				// Only Segments advertise Accept-Ranges
				if tt.target == "segment" {
					assert.Equal(t, "bytes", w.Header().Get("Accept-Ranges"))
				} else {
					assert.Empty(t, w.Header().Get("Accept-Ranges"))
				}
			}

			if tt.wantRange != "" {
				assert.Equal(t, tt.wantRange, w.Header().Get("Content-Range"))
			}
			if tt.wantStatus == http.StatusServiceUnavailable {
				assert.Equal(t, "application/problem+json", w.Header().Get("Content-Type"))
				assert.Contains(t, w.Body.String(), "PREPARING")
				assert.NotEmpty(t, w.Header().Get("Retry-After"))
			}

			if tt.method != "HEAD" && (tt.wantStatus == http.StatusOK || tt.wantStatus == http.StatusPartialContent) {
				assert.Equal(t, tt.wantLen, w.Body.Len())
			}
		})
	}
}
