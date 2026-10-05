// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPipelineHandler_IPTVInboundResolution(t *testing.T) {
	secret := "secret-test-key-of-at-least-32-bytes!!"
	parser, err := sourceref.NewParser([]byte(secret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()
	rawCanary := "4097:0:1:0:0:0:0:0:0:0:http%3a//canary.invalid/live/stream.ts:Canary"
	src, err := parser.Parse(rawCanary)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))
	res := edge.NewResolver(reg, parser, nil)

	opaqueID := string(src.ID())

	var dialedRef string
	cfg := DefaultTestConnectorConfig("", 8001)
	mockDial := func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		dialedRef = key.ServiceRef
		pr, pw := io.Pipe()
		go func() {
			_ = pw.Close()
		}()
		return pr, nil
	}
	cfg.DialFn = mockDial
	cfg.IPTVDialFn = mockDial

	mgr := session.NewManager(session.DefaultManagerConfig(), NewLivePipelineConnector(cfg))
	h := NewHandlerWithReceiver(mgr, "127.0.0.1", 8001)
	h.SetIPTVResolver(res)

	// 1. Opaque known: resolves and attempts acquire with resolved raw ref
	{
		dialedRef = ""
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+opaqueID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		// Handler might return 500 or 502 because the pipe is immediately closed, but dialedRef must be the resolved raw ref!
		assert.Equal(t, rawCanary, dialedRef)
	}

	// 2. Opaque unknown: returns 404 ProblemDetails, zero input echo
	{
		unknownID := "iptv_abcdefghijklmnopqrstuvwxyz"
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+unknownID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.NotContains(t, rec.Body.String(), unknownID)
		assert.Contains(t, rec.Body.String(), problemcode.CodeNotFound)
	}

	// 3. Malformed iptv_ ID: returns 400 ProblemDetails, zero input echo
	{
		malformedID := "iptv_invalid_base32!!!"
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+malformedID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.NotContains(t, rec.Body.String(), malformedID)
		assert.Contains(t, rec.Body.String(), problemcode.CodeInvalidInput)
	}

	// 4. Raw IPTV ref: passes through, dials with raw ref (unescaped by PathUnescape)
	{
		dialedRef = ""
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+rawCanary, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		expected, _ := url.PathUnescape(rawCanary)
		assert.Equal(t, expected, dialedRef)
	}

	// 5. Nil resolver: opaque returns 404, raw IPTV ref dials
	{
		hNil := NewHandlerWithReceiver(mgr, "127.0.0.1", 8001)
		hNil.SetIPTVResolver(nil)

		// Opaque -> 404
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+opaqueID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		hNil.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)

		// Raw -> dials
		dialedRef = ""
		ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel2()
		req2 := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+rawCanary, nil).WithContext(ctx2)
		rec2 := httptest.NewRecorder()
		hNil.ServeHTTP(rec2, req2)
		expected, _ := url.PathUnescape(rawCanary)
		assert.Equal(t, expected, dialedRef)
	}
}

type mockDialTracker struct {
	mu    sync.Mutex
	keys  []session.SessionKey
	dials int
}

func (dt *mockDialTracker) Record(key session.SessionKey) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.keys = append(dt.keys, key)
	dt.dials++
}

func (dt *mockDialTracker) Count() int {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	return dt.dials
}

func (dt *mockDialTracker) FirstKey() session.SessionKey {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	if len(dt.keys) == 0 {
		return session.SessionKey{}
	}
	return dt.keys[0]
}

func TestPipeline_PrepareAndLive_ConsistentSessionKey_IPTV(t *testing.T) {
	capture := tsfixture.Load(t, "verify_final_v3.ts")

	secret := "secret-test-key-of-at-least-32-bytes!!"
	parser, err := sourceref.NewParser([]byte(secret))
	require.NoError(t, err)
	reg := sourceref.NewRegistry()
	iptvRef := "4097:0:1:4E27:0:0:0:0:0:0:http%3a//example.com/stream:Channel"
	src, err := parser.Parse(iptvRef)
	require.NoError(t, err)
	require.NoError(t, reg.Replace([]sourceref.Source{src}))
	res := edge.NewResolver(reg, parser, nil)

	opaqueID := string(src.ID())

	tracker := &mockDialTracker{}
	var (
		upstreamClose sync.Once
		upstreamDone  = make(chan struct{})
	)
	t.Cleanup(func() {
		upstreamClose.Do(func() { close(upstreamDone) })
	})

	mockDial := func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		tracker.Record(key)
		pr, pw := io.Pipe()
		go func() {
			defer func() { _ = pw.Close() }()
			for {
				for i := 0; i < len(capture); i += 188 * 32 {
					end := i + 188*32
					if end > len(capture) {
						end = len(capture)
					}
					select {
					case <-upstreamDone:
						return
					default:
					}
					if _, err := pw.Write(capture[i:end]); err != nil {
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
		}()
		return pr, nil
	}

	cfg := DefaultTestConnectorConfig("", 8001)
	cfg.NormConfig.StartupReservoirMs = 50.0
	cfg.NormConfig.PacerIntervalMs = 5.0
	cfg.NormConfig.InitialBitrateKbps = 20000.0
	cfg.NormConfig.StagingBufferCapacity = 32 * 1024 * 1024
	cfg.DialFn = mockDial
	cfg.IPTVDialFn = mockDial

	mgr := session.NewManager(session.DefaultManagerConfig(), NewLivePipelineConnector(cfg))
	t.Cleanup(func() { _ = mgr.Close() })

	prepMgr := NewPreparationManager(mgr, DefaultPreparationConfig(), zerolog.Nop())
	t.Cleanup(prepMgr.Close)
	prepHandler := NewPrepareHandler(prepMgr, "127.0.0.1", 8001)
	prepHandler.SetIPTVResolver(res)

	liveHandler := NewHandlerWithReceiver(mgr, "127.0.0.1", 8001)
	liveHandler.SetIPTVResolver(res)

	server := httptest.NewServer(liveHandler)
	t.Cleanup(server.Close)

	// 1. Prepare starts for opaque IPTV ref and awaits readiness
	prepResp := startPreparation(t, prepHandler, opaqueID, "client-key-test")
	require.NotEmpty(t, prepResp.PreparationID)

	settled := pollUntilSettled(t, prepHandler, prepResp.PreparationID, "client-key-test", 10*time.Second)
	require.Equal(t, string(PreparationReady), settled.State, "preparation must settle in ready state")

	// Verify the session key used during prepare: TargetProgram must be 0 (auto-detect TS PAT), NOT 0x4E27
	require.Equal(t, 1, tracker.Count(), "expected exactly 1 upstream dial from prepare")
	prepKey := tracker.FirstKey()
	assert.Equal(t, uint16(0), prepKey.TargetProgram, "prepare must force TargetProgram=0 for IPTV")

	// 2. Client initiates GET /api/v3/stream/live/{opaqueID} to verify session reuse and actual media delivery
	reqCtx, reqCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer reqCancel()
	reqLive, err := http.NewRequestWithContext(reqCtx, http.MethodGet, server.URL+"/api/v3/stream/live/"+opaqueID, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(reqLive)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "video/mp2t", resp.Header.Get("Content-Type"))

	// Read initial transport stream chunk to prove media delivery
	chunk := make([]byte, 16*1024)
	n, err := io.ReadFull(resp.Body, chunk)
	require.NoError(t, err)
	assert.Equal(t, len(chunk), n)
	assert.Equal(t, byte(0x47), chunk[0], "stream must deliver TS packets starting with sync byte 0x47")

	// Since session.Manager reuses the active prepared session with matching SessionKey, exactly ONE dial occurred in total
	assert.Equal(t, 1, tracker.Count(), "live request should reuse prepared session without issuing a second upstream dial")

	// 3. For DVB channel, TargetProgram matches the triplet field for both prepare and live
	dvbRef := "1:0:19:4E27:3FB:1:C00000:0:0:0:"
	keyLiveDVB := session.NewSessionKey("127.0.0.1", 8001, dvbRef)
	parts := strings.Split(dvbRef, ":")
	val, _ := strconv.ParseUint(parts[3], 16, 16)
	keyLiveDVB.TargetProgram = uint16(val)

	assert.Equal(t, uint16(0x4E27), keyLiveDVB.TargetProgram)
	assert.Equal(t, uint16(0x4E27), targetProgramFromServiceRef(dvbRef))
}
