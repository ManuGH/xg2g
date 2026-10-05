// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/api"
	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/stream/ingest/pipeline"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBootstrapConfigDir(t *testing.T, iptvSecret string) string {
	t.Helper()
	skipIfNoFFmpeg(t)

	t.Setenv("XG2G_INITIAL_REFRESH", "false")
	t.Setenv("XG2G_STORE_PATH", t.TempDir())
	t.Setenv("XG2G_DECISION_SECRET", "test-decision-secret-for-bootstrap-tests")
	t.Setenv("XG2G_RECORDINGS_TARGET_SIGNING_KEY", "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1")
	t.Setenv("XG2G_API_TOKEN", "test-token-1234567890123456")
	t.Setenv("XG2G_API_TOKEN_SCOPES", "v3:read,v3:write")

	tmpDir, err := os.MkdirTemp("", "xg2g-bootstrap-iptv-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	if iptvSecret != "" {
		t.Setenv("XG2G_IPTV_SOURCE_SECRET", iptvSecret)
	} else {
		t.Setenv("XG2G_IPTV_SOURCE_SECRET", "")
	}

	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
version: v3
dataDir: ` + tmpDir + `
api:
  listenAddr: ":0"
engine:
  tunerSlots: [0]
enigma2:
  baseUrl: http://mock-receiver.invalid
  username: root
  password: "dummy-password"
recordings:
  target_signing_key: "abcdefghijklmnopqrstuvwxyz0123456789ABCDE1"
`
	err = os.WriteFile(configPath, []byte(content), 0600)
	require.NoError(t, err)

	return configPath
}

func TestBootstrap_IPTVResolver_DisabledWhenSecretUnset(t *testing.T) {
	configPath := setupBootstrapConfigDir(t, "")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath)
	require.NoError(t, err)
	defer container.Close()

	// Invariant: secret unset => resolver nil, startup OK
	assert.Nil(t, container.IPTVResolver, "container.IPTVResolver must be nil when secret unset")
	assert.Nil(t, container.Server.IPTVResolver(), "server.IPTVResolver() must be nil when secret unset")
}

func TestBootstrap_IPTVResolver_EnabledWhenSecretSet(t *testing.T) {
	validSecret := "0123456789abcdef0123456789abcdef" // exactly 32 bytes
	configPath := setupBootstrapConfigDir(t, validSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	container, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath)
	require.NoError(t, err)
	defer container.Close()

	// Invariant: secret set => resolver present and wired to server
	assert.NotNil(t, container.IPTVResolver, "container.IPTVResolver must be present when secret set")
	assert.NotNil(t, container.Server.IPTVResolver(), "server.IPTVResolver() must be present when secret set")

	// Resolving a non-IPTV ref behaves as pass-through
	raw, kind, err := container.IPTVResolver.ResolveInbound("intents", "1:0:19:1:1:1:C00000:0:0:0:")
	require.NoError(t, err)
	assert.Equal(t, "1:0:19:1:1:1:C00000:0:0:0:", raw)
	assert.Equal(t, edge.KindPassThrough, kind)
}

func TestBootstrap_IPTVResolver_StartupErrorNamesFieldNeverSecret(t *testing.T) {
	// Directly test that any error in parsing names IPTVSourceSecret and never the secret value
	fakeSecret := "short-secret"
	cfg := wireBootstrapState{
		cfg: config.AppConfig{
			IPTVSourceSecret: fakeSecret,
		},
	}
	_ = cfg

	// When an invalid secret is provided (bypassing earlier validation), WireServices fails naming the field
	secretMarker := "CANARY_SECRET_VAL_DO_NOT_LEAK"
	// Create config with invalid length secret (10 chars < 32 bytes)
	// Note: config loader might reject it, but if it reaches bootstrap, error names field not value
	configPath := setupBootstrapConfigDir(t, secretMarker)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath)
	require.Error(t, err)

	errStr := err.Error()
	// Must name the field
	assert.True(t, strings.Contains(errStr, "IPTVSourceSecret") || strings.Contains(errStr, "iptv_source_secret"),
		"startup error must name configuration field")
	// Must never leak the secret value
	assert.False(t, strings.Contains(errStr, secretMarker),
		"startup error must NEVER contain secret value")
}

type syncStreamRecorder struct {
	mu         sync.Mutex
	header     http.Header
	buf        bytes.Buffer
	statusCode int
}

func newSyncStreamRecorder() *syncStreamRecorder {
	return &syncStreamRecorder{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (r *syncStreamRecorder) Header() http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header
}

func (r *syncStreamRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *syncStreamRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCode = code
}

func (r *syncStreamRecorder) Flush() {}

func (r *syncStreamRecorder) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := make([]byte, r.buf.Len())
	copy(b, r.buf.Bytes())
	return b
}

func (r *syncStreamRecorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}

func (r *syncStreamRecorder) Code() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusCode
}

func TestBootstrap_IPTV_AuthenticatedRouter_To_Ingest(t *testing.T) {
	fixtureData := tsfixture.Load(t, "verify_final_v3.ts")
	require.NotEmpty(t, fixtureData, "fixture verify_final_v3.ts must be available")

	// 1. Setup mock IPTV provider serving valid MPEG-TS fixture with realistic pacing
	var providerHits atomic.Int32
	mockProviderDone := make(chan struct{})
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerHits.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		const chunkSize = 188 * 100
		offset := 0
		for {
			select {
			case <-r.Context().Done():
				return
			case <-mockProviderDone:
				return
			default:
				end := offset + chunkSize
				if end > len(fixtureData) {
					end = len(fixtureData)
				}
				chunk := fixtureData[offset:end]
				offset = end
				if offset >= len(fixtureData) {
					offset = 0
				}
				if _, err := w.Write(chunk); err != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}))
	defer func() {
		close(mockProviderDone)
		mockProvider.CloseClientConnections()
		mockProvider.Close()
	}()

	u, err := url.Parse(mockProvider.URL)
	require.NoError(t, err)

	// Configure outbound policy to permit connection to mock IPTV provider
	t.Setenv("XG2G_OUTBOUND_ENABLED", "true")
	t.Setenv("XG2G_OUTBOUND_ALLOW_HOSTS", u.Hostname())
	t.Setenv("XG2G_OUTBOUND_ALLOW_CIDRS", "127.0.0.1/32")
	t.Setenv("XG2G_OUTBOUND_ALLOW_PORTS", u.Port())
	t.Setenv("XG2G_OUTBOUND_ALLOW_SCHEMES", u.Scheme)

	validSecret := "0123456789abcdef0123456789abcdef" // exactly 32 bytes
	configPath := setupBootstrapConfigDir(t, validSecret)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var serverOpts []api.ServerOption
	if runtime.GOOS != "linux" || config.ResolveMediaCoreBin() == "" {
		serverOpts = append(serverOpts, api.WithLiveConnectorConfigModifier(func(cfg *pipeline.ConnectorConfig) {
			testCfg := pipeline.DefaultTestConnectorConfig(cfg.ReceiverBaseURL, cfg.StreamPort)
			cfg.PipelineFn = testCfg.PipelineFn
		}))
	}

	container, err := WireServices(ctx, "test-v3", "test-commit", "test-date", configPath, serverOpts...)
	require.NoError(t, err)
	defer container.Close()

	// Register IPTV source in the wired registry
	rawURL := mockProvider.URL + "/stream.ts"
	rawRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:BootstrapTestChannel", url.QueryEscape(rawURL))
	src, err := container.iptvParser.Parse(rawRef)
	require.NoError(t, err)
	require.NoError(t, container.iptvRegistry.Replace([]sourceref.Source{src}))

	opaqueID := string(src.ID())
	require.True(t, strings.HasPrefix(opaqueID, "iptv_"))

	serverHandler := container.Server.Handler()

	// 2. Unauthenticated request to authenticated control route returns 401 or 403
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v3/stream/prepare?sref="+url.QueryEscape(opaqueID), nil)
		rec := httptest.NewRecorder()
		serverHandler.ServeHTTP(rec, req)
		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, rec.Code, "unauthenticated request to /api/v3/stream/prepare must be rejected")
	}

	// 3. Authenticated request with Bearer token to authenticated route succeeds
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v3/stream/prepare?sref="+url.QueryEscape(opaqueID), nil)
		req.Header.Set("Authorization", "Bearer test-token-1234567890123456")
		req.Header.Set("X-Xg2g-Client-Id", "test-client-123")
		rec := httptest.NewRecorder()
		serverHandler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusAccepted, rec.Code, "authenticated request to /api/v3/stream/prepare must succeed: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), opaqueID, "response must contain client opaque ID")
		assert.NotContains(t, rec.Body.String(), rawURL, "response must never leak provider URL")
	}

	// 4. Media stream route /api/v3/stream/live/<opaqueID> through full production router:
	// Resolves opaque ID, connects to mock IPTV provider via dialIPTV, and streams MPEG-TS packets
	{
		clientCtx, clientCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer clientCancel()

		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+opaqueID, nil).WithContext(clientCtx)
		w := newSyncStreamRecorder()

		handlerDone := make(chan struct{})
		go func() {
			defer close(handlerDone)
			serverHandler.ServeHTTP(w, req)
		}()

		deadline := time.Now().Add(6 * time.Second)
		for w.Len() < 200*1024 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}

		capturedBytes := w.Bytes()
		require.GreaterOrEqual(t, len(capturedBytes), 100*1024, "must have received at least 100 KB of MPEG-TS stream data")

		assert.Equal(t, http.StatusOK, w.Code())
		assert.Contains(t, w.Header().Get("Content-Type"), "video/mp2t")
		assert.GreaterOrEqual(t, providerHits.Load(), int32(1), "mock IPTV provider must have been hit")

		// Canary assertion: raw URL must never leak into responses or headers
		assert.NotContains(t, string(capturedBytes), rawURL)
		for hKey, hVals := range w.Header() {
			for _, val := range hVals {
				assert.NotContains(t, val, rawURL, "header %s leaked raw URL", hKey)
			}
		}

		// Verify ffmpeg actual video frame decode on the stream captured through the server router
		ffmpegVideoCmd := exec.Command("ffmpeg", "-v", "error", "-i", "pipe:0", "-vframes", "1", "-f", "null", "-")
		ffmpegVideoCmd.Stdin = bytes.NewReader(capturedBytes)
		require.NoError(t, ffmpegVideoCmd.Run(), "ffmpeg must successfully decode video frame from router-served stream")

		// Clean client disconnect
		clientCancel()
		select {
		case <-handlerDone:
		case <-time.After(3 * time.Second):
			t.Fatal("handler did not terminate cleanly after client cancel")
		}
	}

	// 5. Unknown opaque ID returns 404 ProblemDetails with zero input echo
	{
		unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+unknownID, nil)
		rec := httptest.NewRecorder()
		serverHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotContains(t, rec.Body.String(), unknownID)
	}

	// 6. Malformed opaque ID returns 400 ProblemDetails with zero input echo
	{
		malformedID := "iptv_invalid_base32!!!"
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+malformedID, nil)
		rec := httptest.NewRecorder()
		serverHandler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.NotContains(t, rec.Body.String(), malformedID)
	}
}
