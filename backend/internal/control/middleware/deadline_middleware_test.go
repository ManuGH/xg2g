// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package middleware

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/http/deadline"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type writerIdentityProbe struct {
	http.ResponseWriter
}

func TestRuntimeDisabledIsFunctionallyPassive(t *testing.T) {
	policy := deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded}
	originalContext := context.WithValue(context.Background(), struct{}{}, "sentinel")
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(originalContext)
	recorder := httptest.NewRecorder()
	originalWriter := &writerIdentityProbe{ResponseWriter: recorder}

	calls := 0
	handler := WithRoutePolicy(policy, RuntimeDisabled)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Same(t, originalWriter, w)
		assert.Same(t, originalContext, r.Context())
		_, ok := DeadlineStateFromContext(r.Context())
		assert.False(t, ok)
		w.WriteHeader(http.StatusNoContent)
	}))

	handler.ServeHTTP(originalWriter, request)
	assert.Equal(t, 1, calls)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestRuntimeEnforcedFailsClosedWithoutTopLevelState(t *testing.T) {
	called := false
	handler := WithRoutePolicy(
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		RuntimeEnforced,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.False(t, called)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestDeadlineMiddlewarePanicRecoveryStackOrder(t *testing.T) {
	handler := WriteTimeoutMiddleware(deadline.DefaultTimeouts(), RuntimeDisabled)(
		Recoverer(
			WithRoutePolicy(
				deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
				RuntimeDisabled,
			)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("boom")
			})),
		),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestDeadlineMiddlewareCompressionStackOrder(t *testing.T) {
	payload := bytes.Repeat([]byte("compressible response body\n"), 128)
	handler := WriteTimeoutMiddleware(deadline.DefaultTimeouts(), RuntimeDisabled)(
		chimiddleware.Compress(5)(
			WithRoutePolicy(
				deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
				RuntimeDisabled,
			)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write(payload)
			})),
		),
	)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	require.Equal(t, "gzip", recorder.Header().Get("Content-Encoding"))
	reader, err := gzip.NewReader(recorder.Body)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, payload, decompressed)
}

func TestDeadlineMiddlewareResponseCapturerStackOrder(t *testing.T) {
	capturedStatus := 0
	capturer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrapped := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)
			capturedStatus = wrapped.Status()
		})
	}
	handler := WriteTimeoutMiddleware(deadline.DefaultTimeouts(), RuntimeDisabled)(
		capturer(
			WithRoutePolicy(
				deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
				RuntimeDisabled,
			)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			})),
		),
	)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", nil))
	assert.Equal(t, http.StatusAccepted, recorder.Code)
	assert.Equal(t, http.StatusAccepted, capturedStatus)
}
