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
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	"github.com/ManuGH/xg2g/internal/problemcode"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
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
