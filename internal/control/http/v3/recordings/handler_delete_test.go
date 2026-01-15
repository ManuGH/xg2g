package recordings_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/http/v3/recordings"
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
