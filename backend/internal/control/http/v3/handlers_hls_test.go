package v3

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

type playlistSanityExpectation struct {
	expectVOD bool
}

type playlistSanityResult struct {
	format      string
	initURI     string
	segmentURIs []string
}

func TestHLSHandlers_GateY_PlaylistAndMediaSanity(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	writeFile := func(name string, payload []byte) string {
		t.Helper()
		path := filepath.Join(tmpDir, name)
		require.NoError(t, os.WriteFile(path, payload, 0644))
		return path
	}

	tsPath := writeFile("seg_000101.ts", bytes.Repeat([]byte{0x47}, 188))
	initPath := writeFile("init.mp4", []byte("....ftyp...."))
	m4sPath := writeFile("seg_000001.m4s", []byte("....moof....mdat...."))

	tests := []struct {
		name        string
		playlist    string
		expectation playlistSanityExpectation
	}{
		{
			name: "Live_TS",
			playlist: `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:4
#EXT-X-MEDIA-SEQUENCE:101
#EXTINF:4.000,
seg_000101.ts
#EXTINF:4.000,
seg_000102.ts
`,
			expectation: playlistSanityExpectation{expectVOD: false},
		},
		{
			name: "VOD_fMP4",
			playlist: `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:2
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-MAP:URI="init.mp4"
#EXTINF:2.000,
seg_000001.m4s
#EXT-X-ENDLIST
`,
			expectation: playlistSanityExpectation{expectVOD: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recordingID := "rec-gate-y"
			mockRes := new(MockArtifactResolver)
			s := &Server{artifacts: mockRes}

			mockRes.
				On("ResolvePlaylist", mock.Anything, recordingID, mock.Anything).
				Return(artifacts.ArtifactOK{
					Data:    []byte(tt.playlist),
					ModTime: now,
					Kind:    artifacts.ArtifactKindPlaylist,
				}, (*artifacts.ArtifactError)(nil)).
				Once()

			rrPlaylist := httptest.NewRecorder()
			reqPlaylist := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/playlist.m3u8", nil)
			s.GetRecordingHLSPlaylist(rrPlaylist, reqPlaylist, recordingID)

			require.Equal(t, http.StatusOK, rrPlaylist.Code)
			require.Contains(t, []string{"application/vnd.apple.mpegurl", "application/x-mpegURL"}, rrPlaylist.Header().Get("Content-Type"))
			require.Equal(t, "no-store", rrPlaylist.Header().Get("Cache-Control"))
			require.Empty(t, rrPlaylist.Header().Get("Accept-Ranges"))

			sanity := assertGateYPlaylistSanity(t, rrPlaylist.Body.String(), tt.expectation)

			if sanity.initURI != "" {
				mockRes.
					On("ResolveSegment", mock.Anything, recordingID, sanity.initURI).
					Return(artifacts.ArtifactOK{
						AbsPath: initPath,
						ModTime: now,
						Kind:    artifacts.ArtifactKindSegmentInit,
					}, (*artifacts.ArtifactError)(nil)).
					Once()

				rrInit := httptest.NewRecorder()
				reqInit := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/"+sanity.initURI, nil)
				s.GetRecordingHLSCustomSegment(rrInit, reqInit, recordingID, sanity.initURI)

				require.Equal(t, http.StatusOK, rrInit.Code)
				require.Equal(t, "video/mp4", rrInit.Header().Get("Content-Type"))
				require.Equal(t, "public, max-age=3600", rrInit.Header().Get("Cache-Control"))
				require.Equal(t, "bytes", rrInit.Header().Get("Accept-Ranges"))
			}

			firstSegment := sanity.segmentURIs[0]
			segmentPath := tsPath
			segmentKind := artifacts.ArtifactKindSegmentTS
			if sanity.format == "fmp4" {
				segmentPath = m4sPath
				segmentKind = artifacts.ArtifactKindSegmentFMP4
			}

			mockRes.
				On("ResolveSegment", mock.Anything, recordingID, firstSegment).
				Return(artifacts.ArtifactOK{
					AbsPath: segmentPath,
					ModTime: now,
					Kind:    segmentKind,
				}, (*artifacts.ArtifactError)(nil)).
				Once()

			rrSeg := httptest.NewRecorder()
			reqSeg := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/"+recordingID+"/"+firstSegment, nil)
			s.GetRecordingHLSCustomSegment(rrSeg, reqSeg, recordingID, firstSegment)

			require.Equal(t, http.StatusOK, rrSeg.Code)
			if sanity.format == "ts" {
				require.Equal(t, "video/mp2t", rrSeg.Header().Get("Content-Type"))
			} else {
				require.Contains(t, []string{"video/mp4", "video/iso.segment"}, rrSeg.Header().Get("Content-Type"))
			}
			require.Equal(t, "public, max-age=60", rrSeg.Header().Get("Cache-Control"))
			require.Equal(t, "bytes", rrSeg.Header().Get("Accept-Ranges"))
			require.Equal(t, "identity", rrSeg.Header().Get("Content-Encoding"))

			mockRes.AssertExpectations(t)
		})
	}
}

func assertGateYPlaylistSanity(t *testing.T, playlist string, expectation playlistSanityExpectation) playlistSanityResult {
	t.Helper()

	linesRaw := strings.Split(strings.ReplaceAll(playlist, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(linesRaw))
	for _, line := range linesRaw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}

	require.NotEmpty(t, lines)
	require.Equal(t, "#EXTM3U", lines[0], "playlist must begin with #EXTM3U")

	result := playlistSanityResult{}

	hasVersion := false
	hasTargetDuration := false
	hasMediaSequence := false
	hasEndlist := false
	hasPlaylistTypeVOD := false

	for idx, line := range lines {
		switch {
		case strings.HasPrefix(line, "#EXT-X-VERSION:"):
			hasVersion = true
		case strings.HasPrefix(line, "#EXT-X-TARGETDURATION:"):
			hasTargetDuration = true
			raw := strings.TrimSpace(strings.TrimPrefix(line, "#EXT-X-TARGETDURATION:"))
			value, err := strconv.Atoi(raw)
			require.NoError(t, err, "TARGETDURATION must be integer")
			require.Greater(t, value, 0, "TARGETDURATION must be > 0")
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			hasMediaSequence = true
		case strings.EqualFold(line, "#EXT-X-PLAYLIST-TYPE:VOD"):
			hasPlaylistTypeVOD = true
		case line == "#EXT-X-ENDLIST":
			hasEndlist = true
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			result.format = "fmp4"
			result.initURI = extractMapURI(line)
			require.NotEmpty(t, result.initURI, "EXT-X-MAP URI must be present")
			require.True(t, strings.HasSuffix(result.initURI, ".mp4"), "fMP4 init segment must end with .mp4")
		case strings.HasPrefix(line, "#EXTINF:"):
			require.Less(t, idx+1, len(lines), "EXTINF must have a following segment URI")
			next := strings.TrimSpace(lines[idx+1])
			require.NotEmpty(t, next, "segment URI after EXTINF must not be empty")
			require.False(t, strings.HasPrefix(next, "#"), "segment URI must follow EXTINF directly")
			result.segmentURIs = append(result.segmentURIs, next)
		}
	}

	require.True(t, hasVersion, "playlist must contain EXT-X-VERSION")
	require.True(t, hasTargetDuration, "playlist must contain EXT-X-TARGETDURATION")
	require.NotEmpty(t, result.segmentURIs, "playlist must contain at least one segment")

	if expectation.expectVOD {
		require.True(t, hasPlaylistTypeVOD, "VOD playlist must contain EXT-X-PLAYLIST-TYPE:VOD")
		require.True(t, hasEndlist, "VOD playlist must contain EXT-X-ENDLIST")
	} else {
		require.True(t, hasMediaSequence, "live playlist must contain EXT-X-MEDIA-SEQUENCE")
		require.False(t, hasEndlist, "live playlist must not contain EXT-X-ENDLIST")
	}

	if result.format == "" {
		result.format = "ts"
	}
	for _, uri := range result.segmentURIs {
		if result.format == "fmp4" {
			require.True(t, strings.HasSuffix(uri, ".m4s"), "fMP4 playlist segments must end with .m4s")
			continue
		}
		require.True(t, strings.HasSuffix(uri, ".ts"), "TS playlist segments must end with .ts")
	}

	return result
}

func extractMapURI(line string) string {
	const marker = `URI="`
	idx := strings.Index(line, marker)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
