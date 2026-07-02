package v3

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/auth"
	recservice "github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func putResumeState(t *testing.T, server *Server, recordingID, body string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, "/api/v3/recordings/"+recordingID+"/resume", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"}))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("recordingId", recordingID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	server.HandleRecordingResume(rr, req)
	require.Equal(t, http.StatusNoContent, rr.Code)
}

func TestHandleRecordingsContinue_ReturnsRecentUnfinished(t *testing.T) {
	store := resume.NewMemoryStore()
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	server := NewServer(config.AppConfig{}, nil, nil)
	server.SetDependencies(Dependencies{ResumeStore: store})

	firstID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/first.ts")
	secondID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/second.ts")
	finishedID := recservice.EncodeRecordingID("1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/done.ts")

	putResumeState(t, server, firstID, `{"position":600,"total":3600,"title":"First","channel":"ORF1"}`)
	// Force distinguishable updated_at ordering.
	time.Sleep(5 * time.Millisecond)
	putResumeState(t, server, secondID, `{"position":900,"total":5400,"title":"Second","channel":"ZDF HD"}`)
	putResumeState(t, server, finishedID, `{"position":3600,"total":3600,"finished":true,"title":"Done"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/continue", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"})))
	rr := httptest.NewRecorder()
	server.HandleRecordingsContinue(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var payload ContinueWatchingResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &payload))
	require.Len(t, payload.Items, 2)
	require.Equal(t, secondID, payload.Items[0].RecordingID)
	require.Equal(t, "Second", payload.Items[0].Title)
	require.Equal(t, "ZDF HD", payload.Items[0].Channel)
	require.Equal(t, int64(900), payload.Items[0].PosSeconds)
	require.Equal(t, int64(5400), payload.Items[0].DurationSeconds)
	require.Equal(t, firstID, payload.Items[1].RecordingID)
}

func TestHandleRecordingsContinue_RequiresPrincipal(t *testing.T) {
	store := resume.NewMemoryStore()
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	server := NewServer(config.AppConfig{}, nil, nil)
	server.SetDependencies(Dependencies{ResumeStore: store})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/continue", nil)
	rr := httptest.NewRecorder()
	server.HandleRecordingsContinue(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestHandleRecordingsContinue_RejectsInvalidLimit(t *testing.T) {
	store := resume.NewMemoryStore()
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	server := NewServer(config.AppConfig{}, nil, nil)
	server.SetDependencies(Dependencies{ResumeStore: store})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/recordings/continue?limit=nope", nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), auth.NewPrincipal("token", "viewer", []string{"v3:read"})))
	rr := httptest.NewRecorder()
	server.HandleRecordingsContinue(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}
