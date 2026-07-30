package hls

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

type routeTestStore struct {
	record *model.SessionRecord
	err    error
	gets   int
}

func (s *routeTestStore) ListSessions(context.Context) ([]*model.SessionRecord, error) {
	return nil, nil
}

func (s *routeTestStore) GetSession(context.Context, string) (*model.SessionRecord, error) {
	s.gets++
	return s.record, s.err
}

func (s *routeTestStore) UpdateSession(
	context.Context,
	string,
	func(*model.SessionRecord) error,
) (*model.SessionRecord, error) {
	return nil, errors.New("unexpected UpdateSession call")
}

type capturedProblem struct {
	calls  int
	status int
	code   string
}

func (p *capturedProblem) write(
	_ http.ResponseWriter,
	_ *http.Request,
	status int,
	_, _, code, _ string,
	_ map[string]any,
) {
	p.calls++
	p.status = status
	p.code = code
}

func requestWithRouteParams(sessionID, filename string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "http://example.test/v3/hls", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("sessionID", sessionID)
	routeContext.URLParams.Add("filename", filename)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
}

func TestHandleV3HLSRejectsUnsafeRouteInputBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sessionID string
		filename  string
		want      int
	}{
		{name: "unsafe session", sessionID: "../session", filename: "index.m3u8", want: http.StatusBadRequest},
		{name: "path traversal", sessionID: "session_1", filename: "../secret", want: http.StatusForbidden},
		{name: "unlisted artifact", sessionID: "session_1", filename: "secret.txt", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &routeTestStore{}
			problem := &capturedProblem{}
			service := NewService(
				config.AppConfig{},
				store,
				nil,
				problem.write,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
			)

			service.HandleV3HLS(httptest.NewRecorder(), requestWithRouteParams(tt.sessionID, tt.filename))

			if problem.calls != 1 || problem.status != tt.want {
				t.Fatalf("problem = %#v, want one call with status %d", problem, tt.want)
			}
			if store.gets != 0 {
				t.Fatalf("store gets = %d, unsafe route must be rejected first", store.gets)
			}
		})
	}
}

func TestHandleV3HLSPreviewRequiresLiveSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		record     *model.SessionRecord
		wantStatus int
		wantServe  bool
	}{
		{name: "missing", wantStatus: http.StatusNotFound},
		{
			name: "expired",
			record: &model.SessionRecord{
				State:         model.SessionReady,
				ExpiresAtUnix: time.Now().Add(-time.Hour).Unix(),
			},
			wantStatus: http.StatusGone,
		},
		{
			name:       "terminal",
			record:     &model.SessionRecord{State: model.SessionStopped},
			wantStatus: http.StatusGone,
		},
		{
			name:      "ready",
			record:    &model.SessionRecord{State: model.SessionReady},
			wantServe: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &routeTestStore{record: tt.record}
			problem := &capturedProblem{}
			served := false
			service := NewService(
				config.AppConfig{},
				store,
				nil,
				problem.write,
				nil,
				func(http.ResponseWriter, *http.Request, string, int, string, string) {
					served = true
				},
				nil,
				nil,
				nil,
				nil,
			)

			service.HandleV3HLS(httptest.NewRecorder(), requestWithRouteParams("session_1", livePreviewFilename))

			if served != tt.wantServe {
				t.Fatalf("preview served = %v, want %v", served, tt.wantServe)
			}
			if tt.wantServe {
				if problem.calls != 0 {
					t.Fatalf("unexpected problem: %#v", problem)
				}
				return
			}
			if problem.calls != 1 || problem.status != tt.wantStatus {
				t.Fatalf("problem = %#v, want one call with status %d", problem, tt.wantStatus)
			}
		})
	}
}

func TestSafeHLSArtifactAllowlist(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"index.m3u8",
		"stream.m3u8",
		"stream_audio.m3u8",
		"init.mp4",
		"init_video.mp4",
		"seg_0001.ts",
		"seg_video_0001.m4s",
		"stream0001.ts",
	}
	for _, filename := range allowed {
		if !safeHLSFilenameRouteRe.MatchString(filename) {
			t.Errorf("expected %q to be allowed", filename)
		}
	}

	rejected := []string{
		"",
		"preview.jpg",
		"../index.m3u8",
		"seg_1.mp4",
		"stream.m3u8?quality=high",
		"/absolute.ts",
		"segment.ts",
	}
	for _, filename := range rejected {
		if safeHLSFilenameRouteRe.MatchString(filename) {
			t.Errorf("expected %q to be rejected", filename)
		}
	}
}

func TestParseBlockingReloadParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		query    string
		wantMSN  int
		wantPart int
	}{
		{query: "", wantMSN: -1, wantPart: -1},
		{query: "?_HLS_msn=12", wantMSN: 12, wantPart: -1},
		{query: "?_HLS_msn=12&_HLS_part=3", wantMSN: 12, wantPart: 3},
		{query: "?_HLS_part=3", wantMSN: -1, wantPart: -1},
		{query: "?_HLS_msn=-1&_HLS_part=3", wantMSN: -1, wantPart: -1},
		{query: "?_HLS_msn=invalid&_HLS_part=3", wantMSN: -1, wantPart: -1},
		{query: "?_HLS_msn=12&_HLS_part=-1", wantMSN: 12, wantPart: -1},
	}

	for _, tt := range tests {
		request := httptest.NewRequest(http.MethodGet, "http://example.test/hls"+tt.query, nil)
		msn, part := parseBlockingReloadParams(request)
		if msn != tt.wantMSN || part != tt.wantPart {
			t.Errorf("%q: got (%d, %d), want (%d, %d)", tt.query, msn, part, tt.wantMSN, tt.wantPart)
		}
	}
}
