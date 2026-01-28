package recordings_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
	"github.com/ManuGH/xg2g/internal/openwebif"
)

type mockOWIClient struct {
	deleteErr error
}

func (m *mockOWIClient) DeleteMovie(ctx context.Context, sRef string) error {
	return m.deleteErr
}

func TestDeleteRecording_InvalidID(t *testing.T) {
	req := httptest.NewRequest("DELETE", "/recordings/invalid", nil)
	w := httptest.NewRecorder()

	problemCalled := false
	deps := recordings.DeleteDeps{
		NewOWIClient: func() recordings.OpenWebIFClient {
			return &mockOWIClient{deleteErr: errors.New("invalid id at upstream")}
		},
		WriteProblem: func(w http.ResponseWriter, r *http.Request, status int, typ, title, code, detail string) {
			problemCalled = true
			if status != http.StatusInternalServerError {
				t.Errorf("expected status 500, got %d", status)
			}
			if typ != "recordings/delete_failed" {
				t.Errorf("expected type recordings/delete_failed, got %s", typ)
			}
			if code != "DELETE_FAILED" {
				t.Errorf("expected code DELETE_FAILED, got %s", code)
			}
		},
	}

	recordings.DeleteRecording(w, req, "invalid", deps)

	if !problemCalled {
		t.Error("expected WriteProblem to be called because client failed")
	}
}

func TestDeleteRecording_Success(t *testing.T) {
	// Valid ID needs a path segment for validation
	// 1:0:1:/movie.ts -> MTowOjE6L21vdmllLnRz
	validID := "MTowOjE6L21vdmllLnRz"

	req := httptest.NewRequest("DELETE", "/recordings/"+validID, nil)
	w := httptest.NewRecorder()

	deps := recordings.DeleteDeps{
		NewOWIClient: func() recordings.OpenWebIFClient {
			return &mockOWIClient{deleteErr: nil}
		},
		WriteProblem: func(w http.ResponseWriter, r *http.Request, status int, typ, title, code, detail string) {
			t.Errorf("unexpected WriteProblem call: %d %s", status, code)
		},
		Logger: func(msg string, keyvals ...any) {
			// no-op
		},
	}

	recordings.DeleteRecording(w, req, validID, deps)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}
}

func TestDeleteRecording_UpstreamFailure(t *testing.T) {
	validID := "MTowOjE6L21vdmllLnRz"

	req := httptest.NewRequest("DELETE", "/recordings/"+validID, nil)
	w := httptest.NewRecorder()

	problemCalled := false
	deps := recordings.DeleteDeps{
		NewOWIClient: func() recordings.OpenWebIFClient {
			return &mockOWIClient{deleteErr: errors.New("upstream boom")}
		},
		WriteProblem: func(w http.ResponseWriter, r *http.Request, status int, typ, title, code, detail string) {
			problemCalled = true
			if status != http.StatusInternalServerError {
				t.Errorf("expected status 500, got %d", status)
			}
			if typ != "recordings/delete_failed" {
				t.Errorf("expected type recordings/delete_failed, got %s", typ)
			}
			if code != "DELETE_FAILED" {
				t.Errorf("expected code DELETE_FAILED, got %s", code)
			}
		},
		Logger: func(msg string, keyvals ...any) {
			if len(keyvals) == 0 {
				t.Error("expected keyvals in logger")
			}
		},
	}

	recordings.DeleteRecording(w, req, validID, deps)

	if !problemCalled {
		t.Error("expected WriteProblem to be called")
	}
}

func TestDeleteRecording_UpstreamErrors(t *testing.T) {
	validID := "MTowOjE6L21vdmllLnRz"
	req := httptest.NewRequest("DELETE", "/recordings/"+validID, nil)

	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantType   string
		wantCode   string
		wantTitle  string
	}{
		{
			name:       "upstream_404_maps_to_404",
			err:        openwebif.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantType:   "recordings/not-found",
			wantCode:   "NOT_FOUND",
			wantTitle:  "Not Found",
		},
		{
			name:       "upstream_auth_maps_to_403",
			err:        openwebif.ErrForbidden,
			wantStatus: http.StatusForbidden,
			wantType:   "recordings/upstream-auth",
			wantCode:   "UPSTREAM_AUTH",
			wantTitle:  "Upstream Auth Failed",
		},
		{
			name:       "upstream_timeout_maps_to_504",
			err:        openwebif.ErrTimeout,
			wantStatus: http.StatusGatewayTimeout,
			wantType:   "recordings/upstream-timeout",
			wantCode:   "UPSTREAM_TIMEOUT",
			wantTitle:  "Upstream Timeout",
		},
		{
			name:       "upstream_unavailable_maps_to_502",
			err:        openwebif.ErrUpstreamUnavailable,
			wantStatus: http.StatusBadGateway,
			wantType:   "recordings/upstream-unavailable",
			wantCode:   "UPSTREAM_UNAVAILABLE",
			wantTitle:  "Upstream Unavailable",
		},
		{
			name:       "upstream_5xx_maps_to_502",
			err:        openwebif.ErrUpstreamError,
			wantStatus: http.StatusBadGateway,
			wantType:   "recordings/upstream",
			wantCode:   "UPSTREAM_ERROR",
			wantTitle:  "Upstream Error",
		},
		{
			name:       "malformed_upstream_response_maps_to_502",
			err:        openwebif.ErrUpstreamBadResponse,
			wantStatus: http.StatusBadGateway,
			wantType:   "recordings/upstream",
			wantCode:   "UPSTREAM_ERROR",
			wantTitle:  "Upstream Error",
		},
		{
			name:       "unknown_error_maps_to_500",
			err:        errors.New("generic boom"),
			wantStatus: http.StatusInternalServerError,
			wantType:   "recordings/delete_failed",
			wantCode:   "DELETE_FAILED",
			wantTitle:  "Delete Failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			problemCalled := false

			deps := recordings.DeleteDeps{
				NewOWIClient: func() recordings.OpenWebIFClient {
					return &mockOWIClient{deleteErr: tc.err}
				},
				WriteProblem: func(w http.ResponseWriter, r *http.Request, status int, typ, title, code, detail string) {
					problemCalled = true
					if status != tc.wantStatus {
						t.Errorf("expected status %d, got %d", tc.wantStatus, status)
					}
					if typ != tc.wantType {
						t.Errorf("expected type %s, got %s", tc.wantType, typ)
					}
					if code != tc.wantCode {
						t.Errorf("expected code %s, got %s", tc.wantCode, code)
					}
					if title != tc.wantTitle {
						t.Errorf("expected title %s, got %s", tc.wantTitle, title)
					}
					if detail == "" {
						t.Error("expected non-empty detail")
					}
					// Problem details must not leak sensitive info (implicit check here, detail should be localized)
				},
				Logger: func(msg string, keyvals ...any) {
					// no-op
				},
			}

			recordings.DeleteRecording(w, req, validID, deps)

			if !problemCalled {
				t.Error("expected WriteProblem to be called")
			}
		})
	}
}
