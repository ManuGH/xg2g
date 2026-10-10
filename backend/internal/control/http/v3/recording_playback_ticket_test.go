package v3

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/auth"
	v3playbackinfo "github.com/ManuGH/xg2g/internal/control/http/v3/playbackinfo"
	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings/artifacts"
	"github.com/ManuGH/xg2g/internal/household"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func issueRecordingTicketForTest(t *testing.T, s *Server, id string) string {
	t.Helper()
	raw := "/api/v3/recordings/" + id + "/playlist.m3u8?variant=test&target=receipt"
	info := v3playbackinfo.PlaybackInfo{Mode: v3playbackinfo.PlaybackInfoModeHls, Url: &raw,
		Decision: &v3playbackinfo.PlaybackDecision{SelectedOutputUrl: raw, Outputs: []v3playbackinfo.PlaybackOutput{{Kind: "hls", Url: raw}}}}
	r := httptest.NewRequest(http.MethodPost, "https://media.invalid/api/v3/recordings/"+id+"/stream-info", nil)
	r = r.WithContext(auth.WithPrincipal(r.Context(), auth.NewPrincipal("api-token", "usr_test", []string{string(ScopeV3Read)})))
	require.NoError(t, s.ticketRecordingPlaybackInfo(r, id, &info))
	parsed, err := url.Parse(*info.Url)
	require.NoError(t, err)
	ticket := parsed.Query().Get("ticket")
	require.Len(t, ticket, 64)
	require.Equal(t, "test", parsed.Query().Get("variant"))
	require.Equal(t, "receipt", parsed.Query().Get("target"))
	require.Equal(t, *info.Url, info.Decision.SelectedOutputUrl)
	require.Equal(t, *info.Url, info.Decision.Outputs[0].Url)
	return ticket
}

func TestRecordingPlaybackTicket_MediaOnlyAndResourceBound(t *testing.T) {
	s := &Server{}
	ticket := issueRecordingTicketForTest(t, s, "rec-a")
	for _, file := range []string{"playlist.m3u8", "timeshift.m3u8", "stream.mp4", "init.mp4", "init_0.mp4", "seg_00001.ts", "seg_00001.m4s", "seg_00001.cmfv"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := httptest.NewRequest(method, "https://media.invalid/api/v3/recordings/rec-a/"+file+"?ticket="+ticket, nil)
			require.Empty(t, r.Cookies())
			principal, ok := s.playbackTicketPrincipal(r)
			require.True(t, ok, "%s %s", method, file)
			require.Equal(t, "usr_test", principal.ID)
			require.Equal(t, []string{string(ScopeV3Read)}, principal.Scopes)
		}
	}
	for _, endpoint := range []string{
		"/api/v3/recordings/rec-b/playlist.m3u8", "/api/v3/sessions/rec-a/hls/index.m3u8",
		"/api/v3/recordings/rec-a/stream-info", "/api/v3/recordings/rec-a/resume",
		"/api/v3/recordings/rec-a/metadata.json", "/api/v3/recordings/rec-a/../rec-b/seg_1.ts", "/api/v3/system/config",
	} {
		r := httptest.NewRequest(http.MethodGet, "https://media.invalid"+endpoint+"?ticket="+ticket, nil)
		_, ok := s.playbackTicketPrincipal(r)
		require.False(t, ok, endpoint)
	}
	r := httptest.NewRequest(http.MethodDelete, "https://media.invalid/api/v3/recordings/rec-a/stream.mp4?ticket="+ticket, nil)
	_, ok := s.playbackTicketPrincipal(r)
	require.False(t, ok)
}

func TestRecordingPlaybackTicket_NoUnauthenticatedIssueAndNoCrossResourceLeak(t *testing.T) {
	s := &Server{}
	raw := "/api/v3/recordings/rec-a/stream.mp4"
	info := v3playbackinfo.PlaybackInfo{Mode: v3playbackinfo.PlaybackInfoModeDirectMp4, Url: &raw}
	r := httptest.NewRequest(http.MethodPost, "https://media.invalid/", nil)
	require.Error(t, s.ticketRecordingPlaybackInfo(r, "rec-a", &info))
	require.NotContains(t, *info.Url, "ticket=")
	token := issueRecordingTicketForTest(t, s, "rec-a")
	issued, ok := s.playbackTicketStoreOrDefault().resolve(token, time.Now())
	require.True(t, ok)
	_, ok = s.playbackTicketStoreOrDefault().resolve(token, issued.expiresAt.Add(time.Second))
	require.False(t, ok)
}

func TestRecordingPlaybackTicket_RewriteConfinesCredentials(t *testing.T) {
	ticket := strings.Repeat("a", 64)
	source := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4?variant=test\"\nseg_00001.m4s?variant=test\n/api/v3/recordings/rec-b/seg_1.ts\nhttps://foreign.invalid/seg_1.ts\n//foreign.invalid/seg_1.ts\n../rec-b/seg_1.ts\n#EXT-X-ENDLIST\n"
	got := string(rewriteRecordingPlaylistTicket([]byte(source), "rec-a", ticket))
	require.Contains(t, got, "init.mp4?ticket="+ticket+"&variant=test")
	require.Contains(t, got, "seg_00001.m4s?ticket="+ticket+"&variant=test")
	for _, unchanged := range []string{"/api/v3/recordings/rec-b/seg_1.ts\n", "https://foreign.invalid/seg_1.ts\n", "//foreign.invalid/seg_1.ts\n", "../rec-b/seg_1.ts\n"} {
		require.Contains(t, got, unchanged)
	}
	require.Equal(t, 2, strings.Count(got, "ticket="))
}

func TestRecordingPlaybackTicket_CookieFreePlaylistAndSegments(t *testing.T) {
	for _, fileBacked := range []bool{false, true} {
		t.Run(strconv.FormatBool(fileBacked), func(t *testing.T) {
			resolver := new(MockArtifactResolver)
			s := &Server{artifacts: resolver}
			id := "rec-a"
			ticket := issueRecordingTicketForTest(t, s, id)
			dir := t.TempDir()
			manifest := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\nseg_1.m4s\n#EXT-X-ENDLIST\n"
			artifact := artifacts.ArtifactOK{Data: []byte(manifest), Kind: artifacts.ArtifactKindPlaylist}
			if fileBacked {
				artifact.Data = nil
				artifact.AbsPath = filepath.Join(dir, "index.m3u8")
				require.NoError(t, os.WriteFile(artifact.AbsPath, []byte(manifest), 0600))
			}
			resolver.On("ResolvePlaylist", mock.Anything, id, "", "", mock.Anything).Return(artifact, (*artifacts.ArtifactError)(nil))
			handler := s.authMiddlewareImpl(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.GetRecordingHLSPlaylist(w, r, id) }))
			get := httptest.NewRecorder()
			handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "https://media.invalid/api/v3/recordings/rec-a/playlist.m3u8?ticket="+ticket, nil))
			require.Equal(t, http.StatusOK, get.Code, get.Body.String())
			require.Contains(t, get.Body.String(), "seg_1.m4s?ticket="+ticket)
			require.Contains(t, get.Body.String(), "init.mp4?ticket="+ticket)
			require.Equal(t, strconv.Itoa(get.Body.Len()), get.Header().Get("Content-Length"))
			require.Equal(t, "no-store, private", get.Header().Get("Cache-Control"))
			head := httptest.NewRecorder()
			s.authMiddlewareImpl(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.GetRecordingHLSPlaylistHead(w, r, id) })).ServeHTTP(head,
				httptest.NewRequest(http.MethodHead, "https://media.invalid/api/v3/recordings/rec-a/playlist.m3u8?ticket="+ticket, nil))
			require.Equal(t, http.StatusOK, head.Code)
			require.Empty(t, head.Body.String())
			require.Equal(t, get.Header().Get("Content-Length"), head.Header().Get("Content-Length"))
			for _, file := range []string{"init.mp4", "seg_1.m4s"} {
				segmentPath := filepath.Join(dir, file)
				require.NoError(t, os.WriteFile(segmentPath, []byte("media bytes"), 0600))
				resolver.On("ResolveSegment", mock.Anything, id, file, "").Return(artifacts.ArtifactOK{AbsPath: segmentPath, Kind: artifacts.ArtifactKindSegmentFMP4}, (*artifacts.ArtifactError)(nil))
				w := httptest.NewRecorder()
				s.authMiddlewareImpl(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.GetRecordingHLSCustomSegment(w, r, id, file) })).ServeHTTP(w,
					httptest.NewRequest(http.MethodGet, "https://media.invalid/api/v3/recordings/rec-a/"+file+"?ticket="+ticket, nil))
				require.Equal(t, http.StatusOK, w.Code)
				require.Equal(t, "media bytes", w.Body.String())
			}
			// A ticket carries the already-authorized profile into cookie-free media
			// requests; no-header household fallback cannot replace it with adult access.
			r := httptest.NewRequest(http.MethodGet, "https://media.invalid/api/v3/recordings/rec-a/playlist.m3u8?ticket="+ticket, nil)
			ctx := s.recordingTicketContext(r.WithContext(household.WithProfile(r.Context(), ptrProfile(household.CreateRestrictedProfile()))))
			require.NotEqual(t, household.ProfileKindChild, household.ProfileFromContext(ctx).Kind)
		})
	}
}
func ptrProfile(profile household.Profile) *household.Profile { return &profile }
