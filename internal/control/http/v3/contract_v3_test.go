// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/domain/session/model"
	v3store "github.com/ManuGH/xg2g/internal/domain/session/store"
	v3api "github.com/ManuGH/xg2g/internal/pipeline/api"
	v3bus "github.com/ManuGH/xg2g/internal/pipeline/bus"
	"github.com/ManuGH/xg2g/internal/pipeline/lease"
	"github.com/ManuGH/xg2g/internal/pipeline/resume"
)

const contractFixtureDir = "testdata/v3_contract"

var (
	openapiOnce sync.Once
	openapiDoc  *openapi3.T
	openapiErr  error
)

func loadOpenAPIDoc(t *testing.T) *openapi3.T {
	t.Helper()
	openapiOnce.Do(func() {
		specPath := "openapi.yaml"
		loader := openapi3.NewLoader()
		loader.IsExternalRefsAllowed = true
		doc, err := loader.LoadFromFile(specPath)
		if err != nil {
			openapiErr = err
			return
		}
		if err := doc.Validate(context.Background()); err != nil {
			openapiErr = err
			return
		}
		openapiDoc = doc
	})
	if openapiErr != nil {
		t.Fatalf("openapi load failed: %v", openapiErr)
	}
	return openapiDoc
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(contractFixtureDir, name)
	// #nosec G304
	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err, "read fixture %s", name)
	return data
}

func decodeIntentResponse(t *testing.T, body []byte) v3api.IntentResponse {
	t.Helper()
	var resp v3api.IntentResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func validateOpenAPIResponse(t *testing.T, doc *openapi3.T, req *http.Request, rr *httptest.ResponseRecorder, opts *openapi3filter.Options) {
	t.Helper()
	router, err := legacy.NewRouter(doc)
	require.NoError(t, err, "openapi router init")

	route, pathParams, err := router.FindRoute(req)
	require.NoError(t, err, "openapi route lookup")

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status:  rr.Code,
		Header:  rr.Header(),
		Options: opts,
	}
	input.SetBodyBytes(rr.Body.Bytes())

	require.NoError(t, openapi3filter.ValidateResponse(context.Background(), input), "openapi response validation")
}

func newV3TestServer(t *testing.T, hlsRoot string) (*Server, *v3store.MemoryStore) {
	t.Helper()
	cfg := config.AppConfig{
		APIToken:       "test-token",
		APITokenScopes: []string{string(ScopeV3Write), string(ScopeV3Read)},
		DataDir:        t.TempDir(),
		HLS: config.HLSConfig{
			Root: hlsRoot,
		},
		Engine: config.EngineConfig{
			TunerSlots: []int{0},
		},
	}
	s := NewServer(cfg, nil, nil)
	st := v3store.NewMemoryStore()
	// Inject dependencies
	b := v3bus.NewMemoryBus()
	// store := st // This line is effectively replaced by using 'st' directly
	rs := resume.NewMemoryStore()
	s.SetDependencies(
		b, st, rs, nil, nil, nil, nil, nil, nil, nil, // P3: VODResolver
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	return s, st
}

func TestV3Contract_Intents(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	body := readFixture(t, "post_intents_request.json")

	req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	HandlerWithOptions(s, ChiServerOptions{
		BaseURL: "/api/v3",
	}).ServeHTTP(rr, req)

	require.Equal(t, http.StatusAccepted, rr.Code)
	validateOpenAPIResponse(t, loadOpenAPIDoc(t), req, rr, nil)

	var got map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))

	rawID, ok := got["sessionId"].(string)
	require.True(t, ok, "sessionId missing")
	require.NoError(t, validateUUID(rawID))

	session, err := st.GetSession(context.Background(), rawID)
	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, "corr-intent-001", session.CorrelationID)

	got["sessionId"] = "<sessionId>"
	gotJSON, err := json.Marshal(got)
	require.NoError(t, err)

	require.JSONEq(t, string(readFixture(t, "post_intents_response.json")), string(gotJSON))
}

func TestV3Contract_IntentLeaseBusy(t *testing.T) {
	t.Run("phase2_idempotent_replay", func(t *testing.T) {
		s, st := newV3TestServer(t, t.TempDir())
		// No legacy API Locking

		_, ok, err := st.TryAcquireLease(context.Background(), lease.LeaseKeyTunerSlot(0), "busy-owner", 5*time.Second)
		require.NoError(t, err)
		require.True(t, ok)

		bus, ok := s.v3Bus.(*v3bus.MemoryBus)
		require.True(t, ok)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		sub, err := bus.Subscribe(ctx, string(model.EventStartSession))
		require.NoError(t, err)
		defer func() { _ = sub.Close() }()

		body := readFixture(t, "post_intents_request.json")

		send := func() (*http.Request, *httptest.ResponseRecorder, v3api.IntentResponse) {
			req := httptest.NewRequest(http.MethodPost, "/api/v3/intents", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			HandlerWithOptions(s, ChiServerOptions{
				BaseURL: "/api/v3",
			}).ServeHTTP(rr, req)
			return req, rr, decodeIntentResponse(t, rr.Body.Bytes())
		}

		req1, rr1, resp1 := send()
		require.Equal(t, http.StatusAccepted, rr1.Code)
		validateOpenAPIResponse(t, loadOpenAPIDoc(t), req1, rr1, nil)

		req2, rr2, resp2 := send()
		require.Equal(t, http.StatusAccepted, rr2.Code)
		validateOpenAPIResponse(t, loadOpenAPIDoc(t), req2, rr2, nil)

		require.Equal(t, resp1.SessionID, resp2.SessionID)
		require.Equal(t, "accepted", resp1.Status)
		require.Equal(t, "idempotent_replay", resp2.Status)
		require.Equal(t, "corr-intent-001", resp1.CorrelationID)
		require.Equal(t, resp1.CorrelationID, resp2.CorrelationID)

		select {
		case <-sub.C():
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected session.start event")
		}

		select {
		case <-sub.C():
			t.Fatal("unexpected extra session.start event on replay")
		case <-time.After(50 * time.Millisecond):
		}

		sessions, err := st.ListSessions(context.Background())
		require.NoError(t, err)
		require.Len(t, sessions, 1)
	})
}

func TestV3Contract_SessionState(t *testing.T) {
	s, st := newV3TestServer(t, t.TempDir())
	sessionID := "550e8400-e29b-41d4-a716-446655440000"
	serviceRef := "1:0:1:445D:453:1:C00000:0:0:0:"
	updatedAtUnix := int64(1700000000)

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:     sessionID,
		ServiceRef:    serviceRef,
		Profile:       model.ProfileSpec{Name: "high"},
		State:         model.SessionReady,
		Reason:        model.RNone,
		ReasonDetail:  "baseline",
		CorrelationID: "corr-test-001",
		UpdatedAtUnix: updatedAtUnix,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer test-token")

	handler := HandlerWithOptions(s, ChiServerOptions{
		BaseURL: "/api/v3",
	})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, string(readFixture(t, "get_session_response.json")), rr.Body.String())

	validateOpenAPIResponse(t, loadOpenAPIDoc(t), req, rr, nil)
}

func TestV3Contract_ReadyImpliesPlayable(t *testing.T) {
	hlsRoot := t.TempDir()
	s, st := newV3TestServer(t, hlsRoot)
	sessionID := "550e8400-e29b-41d4-a716-446655440000"

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:  sessionID,
		State:      model.SessionReady,
		ServiceRef: "1:0:1:445D:453:1:C00000:0:0:0:",
		Profile:    model.ProfileSpec{Name: "high"},
	}))

	_, _, _ = writeHLSFixtures(t, hlsRoot, sessionID)

	req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer test-token")

	rr := httptest.NewRecorder()
	handler := HandlerWithOptions(s, ChiServerOptions{
		BaseURL: "/api/v3",
	})
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	validateOpenAPIResponse(t, loadOpenAPIDoc(t), req, rr, nil)

	var sessionResp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &sessionResp))
	state, _ := sessionResp["state"].(string)
	require.Equal(t, "READY", state)

	reqPlaylist := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/index.m3u8", nil)
	reqPlaylist.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rrPlaylist := httptest.NewRecorder()
	handler.ServeHTTP(rrPlaylist, reqPlaylist)

	require.Equal(t, http.StatusOK, rrPlaylist.Code)
	segmentURI := firstSegmentURI(t, rrPlaylist.Body.Bytes())

	reqSeg := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/"+segmentURI, nil)
	reqSeg.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rrSeg := httptest.NewRecorder()
	handler.ServeHTTP(rrSeg, reqSeg)

	require.Equal(t, http.StatusOK, rrSeg.Code)
	require.NotEmpty(t, rrSeg.Body.Bytes())
}

func TestV3Contract_HLS(t *testing.T) {
	hlsRoot := t.TempDir()
	s, st := newV3TestServer(t, hlsRoot)
	sessionID := "550e8400-e29b-41d4-a716-446655440000"

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:  sessionID,
		State:      model.SessionReady,
		ServiceRef: "1:0:1:445D:453:1:C00000:0:0:0:",
		Profile:    model.ProfileSpec{Name: "high"},
	}))

	playlist, initSeg, mediaSeg := writeHLSFixtures(t, hlsRoot, sessionID)

	handler := HandlerWithOptions(s, ChiServerOptions{
		BaseURL: "/api/v3",
	})
	doc := loadOpenAPIDoc(t)

	reqPlaylist := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/index.m3u8", nil)
	reqPlaylist.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rrPlaylist := httptest.NewRecorder()
	handler.ServeHTTP(rrPlaylist, reqPlaylist)

	require.Equal(t, http.StatusOK, rrPlaylist.Code)
	require.Equal(t, "application/vnd.apple.mpegurl", rrPlaylist.Header().Get("Content-Type"))
	require.Equal(t, playlist, rrPlaylist.Body.Bytes())
	validateOpenAPIResponse(t, doc, reqPlaylist, rrPlaylist, &openapi3filter.Options{
		ExcludeResponseBody: true,
	})

	reqTS := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/seg_000000.ts", nil)
	reqTS.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rrTS := httptest.NewRecorder()
	handler.ServeHTTP(rrTS, reqTS)

	require.Equal(t, http.StatusOK, rrTS.Code)
	require.Equal(t, "video/mp2t", rrTS.Header().Get("Content-Type"))
	require.NotEmpty(t, rrTS.Body.Bytes())
	validateOpenAPIResponse(t, doc, reqTS, rrTS, &openapi3filter.Options{
		ExcludeResponseBody: true,
	})

	reqInit := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/init.mp4", nil)
	reqInit.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rrInit := httptest.NewRecorder()
	handler.ServeHTTP(rrInit, reqInit)

	require.Equal(t, http.StatusOK, rrInit.Code)
	require.Equal(t, "video/mp4", rrInit.Header().Get("Content-Type"))
	require.Equal(t, initSeg, rrInit.Body.Bytes())
	require.True(t, bytes.Contains(rrInit.Body.Bytes(), []byte("ftyp")))
	validateOpenAPIResponse(t, doc, reqInit, rrInit, &openapi3filter.Options{
		ExcludeResponseBody: true,
	})

	reqM4s := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/seg_000001.m4s", nil)
	reqM4s.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rrM4s := httptest.NewRecorder()
	handler.ServeHTTP(rrM4s, reqM4s)

	require.Equal(t, http.StatusOK, rrM4s.Code)
	require.Equal(t, "video/mp4", rrM4s.Header().Get("Content-Type"))
	require.Equal(t, mediaSeg, rrM4s.Body.Bytes())
	require.True(t, bytes.Contains(rrM4s.Body.Bytes(), []byte("moof")))
	require.True(t, bytes.Contains(rrM4s.Body.Bytes(), []byte("mdat")))
	validateOpenAPIResponse(t, doc, reqM4s, rrM4s, &openapi3filter.Options{
		ExcludeResponseBody: true,
	})
}

func TestV3Contract_HLSRange(t *testing.T) {
	hlsRoot := t.TempDir()
	s, st := newV3TestServer(t, hlsRoot)
	sessionID := "550e8400-e29b-41d4-a716-446655440000"

	require.NoError(t, st.PutSession(context.Background(), &model.SessionRecord{
		SessionID:  sessionID,
		State:      model.SessionReady,
		ServiceRef: "1:0:1:445D:453:1:C00000:0:0:0:",
		Profile:    model.ProfileSpec{Name: "high"},
	}))

	_, initSeg, _ := writeHLSFixtures(t, hlsRoot, sessionID)
	require.GreaterOrEqual(t, len(initSeg), 2, "init segment must be at least 2 bytes")

	handler := HandlerWithOptions(s, ChiServerOptions{
		BaseURL: "/api/v3",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v3/sessions/"+sessionID+"/hls/init.mp4", nil)
	req.Header.Set("Range", "bytes=0-1")
	req.AddCookie(&http.Cookie{Name: "xg2g_session", Value: "test-token"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusPartialContent, rr.Code)
	require.Equal(t, "video/mp4", rr.Header().Get("Content-Type"))
	require.True(t, strings.HasPrefix(rr.Header().Get("Content-Range"), "bytes 0-1/"))
	require.Equal(t, initSeg[:2], rr.Body.Bytes())
}

func validateUUID(value string) error {
	_, err := uuid.Parse(value)
	return err
}

func writeHLSFixtures(t *testing.T, hlsRoot, sessionID string) (playlist []byte, initSeg []byte, mediaSeg []byte) {
	t.Helper()

	sessionDir := filepath.Join(hlsRoot, "sessions", sessionID)
	require.NoError(t, os.MkdirAll(sessionDir, 0750))

	playlist = readFixture(t, "hls_index_response.m3u8")
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "index.m3u8"), playlist, 0600))

	initSeg = readFixture(t, "hls_init_segment.bin")
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "init.mp4"), initSeg, 0600))

	mediaSeg = readFixture(t, "hls_media_segment.bin")
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "seg_000001.m4s"), mediaSeg, 0600))

	tsSeg := []byte{0x47, 0x40, 0x00, 0x10, 0x00}
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "seg_000000.ts"), tsSeg, 0600))

	return playlist, initSeg, mediaSeg
}

func firstSegmentURI(t *testing.T, playlist []byte) string {
	t.Helper()

	scanner := bufio.NewScanner(bytes.NewReader(playlist))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	require.NoError(t, scanner.Err())
	t.Fatal("no segment URI found in playlist")
	return ""
}
