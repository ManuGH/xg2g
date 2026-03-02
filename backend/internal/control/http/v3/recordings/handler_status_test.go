package recordings

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/vod"
)

func TestWriteStatus_InvalidID(t *testing.T) {
	req := httptest.NewRequest("GET", "/recordings/INVALID_ID/status", nil)
	w := httptest.NewRecorder()

	deps := StatusDeps{
		HLSRoot: func() string { return "/tmp" },
		RecordingCacheDir: func(hlsRoot, serviceRef string) (string, error) {
			return "/tmp/cache", nil
		},
		VODManager: &mockVODManager{},
		WriteError: func(w http.ResponseWriter, r *http.Request, serviceRef string, err error) {
			w.WriteHeader(http.StatusNotFound)
		},
	}

	WriteStatus(w, req, "!!INVALID!!", deps)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

type mockVODManager struct{}

func (m *mockVODManager) Get(ctx context.Context, cacheDir string) (*vod.JobStatus, bool) {
	return nil, false
}
func (m *mockVODManager) GetMetadata(serviceRef string) (vod.Metadata, bool) {
	return vod.Metadata{}, false
}

func TestWriteStatus_Success(t *testing.T) {
	validRef := "1:0:1:0:0:0:0:0:0:0:/movie.ts"
	validID := EncodeRecordingID(validRef)

	req := httptest.NewRequest("GET", "/recordings/"+validID+"/status", nil)
	w := httptest.NewRecorder()

	deps := StatusDeps{
		HLSRoot:    func() string { return "/tmp" },
		VODManager: &mockVODManager{},
		RecordingCacheDir: func(hlsRoot, serviceRef string) (string, error) {
			return "/tmp/cache", nil
		},
		WriteError: func(w http.ResponseWriter, r *http.Request, serviceRef string, err error) {
			t.Errorf("unexpected error: %v", err)
		},
	}

	WriteStatus(w, req, validID, deps)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
