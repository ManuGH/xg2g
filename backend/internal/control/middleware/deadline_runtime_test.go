// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/deadline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deadlineProbeWriter struct {
	header        http.Header
	body          bytes.Buffer
	status        int
	deadlines     []time.Time
	readFromCalls int
	flushCalls    int
	hijackConn    net.Conn
	hijackErr     error
	mu            sync.Mutex
}

func newDeadlineProbeWriter() *deadlineProbeWriter {
	return &deadlineProbeWriter{header: make(http.Header)}
}

func (w *deadlineProbeWriter) Header() http.Header {
	return w.header
}

func (w *deadlineProbeWriter) WriteHeader(code int) {
	w.status = code
}

func (w *deadlineProbeWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *deadlineProbeWriter) SetWriteDeadline(value time.Time) error {
	w.mu.Lock()
	w.deadlines = append(w.deadlines, value)
	w.mu.Unlock()
	return nil
}

func (w *deadlineProbeWriter) Flush() {
	w.flushCalls++
}

func (w *deadlineProbeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if w.hijackErr != nil {
		return nil, nil, w.hijackErr
	}
	if w.hijackConn == nil {
		return nil, nil, errors.New("probe hijack connection missing")
	}
	return w.hijackConn, bufio.NewReadWriter(bufio.NewReader(w.hijackConn), bufio.NewWriter(w.hijackConn)), nil
}

func (w *deadlineProbeWriter) Push(string, *http.PushOptions) error {
	return nil
}

func (w *deadlineProbeWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFromCalls++
	return io.Copy(&w.body, r)
}

func (w *deadlineProbeWriter) deadlineSnapshot() []time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Time(nil), w.deadlines...)
}

type readerOnly struct {
	reader io.Reader
}

func (r readerOnly) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

type trackingConn struct {
	net.Conn
	mu             sync.Mutex
	writeDeadlines []time.Time
}

func (c *trackingConn) SetWriteDeadline(value time.Time) error {
	c.mu.Lock()
	c.writeDeadlines = append(c.writeDeadlines, value)
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(value)
}

func (c *trackingConn) deadlineSnapshot() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.writeDeadlines...)
}

func enforcedHandler(
	timeouts deadline.DeadlineTimeouts,
	policy deadline.RoutePolicy,
	next http.Handler,
) http.Handler {
	return WriteTimeoutMiddleware(timeouts, RuntimeEnforced)(
		WithRoutePolicy(policy, RuntimeEnforced)(next),
	)
}

func TestBoundedPoliciesSetAndResetFixedDeadline(t *testing.T) {
	timeouts := deadline.DeadlineTimeouts{
		APIWriteTimeout:      2 * time.Second,
		MediaWriteTimeout:    7 * time.Second,
		StreamingIdleTimeout: 3 * time.Second,
	}
	for _, test := range []struct {
		name    string
		policy  deadline.RoutePolicy
		timeout time.Duration
	}{
		{"api", deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded}, timeouts.APIWriteTimeout},
		{"media", deadline.RoutePolicy{Class: deadline.RouteDeadlineMediaBounded}, timeouts.MediaWriteTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := newDeadlineProbeWriter()
			before := time.Now()
			handler := enforcedHandler(timeouts, test.policy, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte("ok"))
				require.NoError(t, err)
			}))

			handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
			after := time.Now()
			operations := writer.deadlineSnapshot()
			require.Len(t, operations, 2)
			assert.False(t, operations[0].IsZero())
			assert.False(t, operations[0].Before(before.Add(test.timeout)))
			assert.False(t, operations[0].After(after.Add(test.timeout)))
			assert.True(t, operations[1].IsZero(), "keep-alive deadline must be reset")
		})
	}
}

func TestStreamingPolicyRenewsBeforeWriteFlushAndReaderFrom(t *testing.T) {
	timeouts := deadline.DeadlineTimeouts{
		APIWriteTimeout:      time.Second,
		MediaWriteTimeout:    2 * time.Second,
		StreamingIdleTimeout: 3 * time.Second,
	}
	writer := newDeadlineProbeWriter()
	policy := deadline.RoutePolicy{Class: deadline.RouteDeadlineStreaming, RequiresFlush: true}
	handler := enforcedHandler(timeouts, policy, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("one"))
		require.NoError(t, err)
		require.NoError(t, http.NewResponseController(w).Flush())
		_, err = io.Copy(w, readerOnly{reader: strings.NewReader("two")})
		require.NoError(t, err)
	}))

	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	operations := writer.deadlineSnapshot()
	require.Len(t, operations, 5, "bind + Write + Flush + ReaderFrom.Write + reset")
	for _, operation := range operations[:len(operations)-1] {
		assert.False(t, operation.IsZero())
	}
	assert.True(t, operations[len(operations)-1].IsZero())
	assert.Equal(t, 0, writer.readFromCalls, "underlying ReaderFrom must never bypass rolling renewal")
	assert.Equal(t, 1, writer.flushCalls)
	assert.Equal(t, "onetwo", writer.body.String())
}

func TestSuccessfulHijackClearsDeadlineAndSkipsRequestReset(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	tracked := &trackingConn{Conn: serverConn}
	writer := newDeadlineProbeWriter()
	writer.hijackConn = tracked
	policy := deadline.RoutePolicy{
		Class:                deadline.RouteDeadlineMediaBounded,
		MayUpgradePerRequest: true,
	}
	var state *DeadlineState
	handler := enforcedHandler(deadline.DefaultTimeouts(), policy, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state, _ = DeadlineStateFromContext(r.Context())
		conn, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.Same(t, tracked, conn)
	}))

	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	require.NotNil(t, state)
	assert.True(t, state.IsHijacked())
	require.Len(t, writer.deadlineSnapshot(), 1, "request defer must not touch a hijacked writer")
	connDeadlines := tracked.deadlineSnapshot()
	require.Len(t, connDeadlines, 1)
	assert.True(t, connDeadlines[0].IsZero())
	require.NoError(t, tracked.Close())
}

func TestFailedHijackRetainsBoundedDeadlineUntilRequestReset(t *testing.T) {
	writer := newDeadlineProbeWriter()
	writer.hijackErr = errors.New("upgrade rejected")
	policy := deadline.RoutePolicy{
		Class:                deadline.RouteDeadlineMediaBounded,
		MayUpgradePerRequest: true,
	}
	handler := enforcedHandler(deadline.DefaultTimeouts(), policy, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := w.(http.Hijacker).Hijack()
		require.ErrorContains(t, err, "upgrade rejected")
	}))

	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	operations := writer.deadlineSnapshot()
	require.Len(t, operations, 2)
	assert.False(t, operations[0].IsZero())
	assert.True(t, operations[1].IsZero())
}

func TestHijackDeniedForRouteWithoutUpgradePolicy(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()
	writer := newDeadlineProbeWriter()
	writer.hijackConn = serverConn
	policy := deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded}
	handler := enforcedHandler(deadline.DefaultTimeouts(), policy, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _, err := w.(http.Hijacker).Hijack()
		require.ErrorContains(t, err, "does not permit")
	}))

	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Len(t, writer.deadlineSnapshot(), 2)
}

func TestHTTP2WriterDoesNotExposeHijacker(t *testing.T) {
	writer := newDeadlineProbeWriter()
	policy := deadline.RoutePolicy{Class: deadline.RouteDeadlineMediaBounded}
	handler := enforcedHandler(deadline.DefaultTimeouts(), policy, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, hijacker := w.(http.Hijacker)
		assert.False(t, hijacker)
		_, flusher := w.(http.Flusher)
		assert.True(t, flusher)
		_, pusher := w.(http.Pusher)
		assert.True(t, pusher)
	}))
	request := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	request.ProtoMajor = 2
	request.ProtoMinor = 0

	handler.ServeHTTP(writer, request)
}

func TestRuntimeEnforcedAllowsNonNetworkResponseRecorder(t *testing.T) {
	handler := enforcedHandler(
		deadline.DefaultTimeouts(),
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state, ok := DeadlineStateFromContext(r.Context())
			require.True(t, ok)
			assert.False(t, state.DeadlineSupported())
			w.WriteHeader(http.StatusNoContent)
		}),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestInvalidDeadlineConfigurationFailsClosed(t *testing.T) {
	called := false
	handler := WriteTimeoutMiddleware(deadline.DeadlineTimeouts{}, RuntimeEnforced)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		}),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.False(t, called)
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func TestCanonicalEnforcedStackCompletesCompressionBeforeDeadlineReset(t *testing.T) {
	payload := bytes.Repeat([]byte("compressible response body\n"), 128)
	router := NewRouter(StackConfig{
		EnableCompression:   true,
		DeadlineRuntimeMode: RuntimeEnforced,
		DeadlineTimeouts:    deadline.DefaultTimeouts(),
	})
	router.Get("/", WithRoutePolicy(
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		RuntimeEnforced,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, err := w.Write(payload)
		require.NoError(t, err)
	})).ServeHTTP)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	writer := newDeadlineProbeWriter()
	router.ServeHTTP(writer, request)

	require.Equal(t, "gzip", writer.Header().Get("Content-Encoding"))
	reader, err := gzip.NewReader(bytes.NewReader(writer.body.Bytes()))
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, payload, decompressed)
	operations := writer.deadlineSnapshot()
	require.Len(t, operations, 2, "bind and reset must enclose the compressor lifecycle")
	assert.False(t, operations[0].IsZero())
	assert.True(t, operations[1].IsZero())
}

func TestCanonicalEnforcedStackRecoversPanicBeforeDeadlineReset(t *testing.T) {
	router := NewRouter(StackConfig{
		DeadlineRuntimeMode: RuntimeEnforced,
		DeadlineTimeouts:    deadline.DefaultTimeouts(),
	})
	router.Get("/", WithRoutePolicy(
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		RuntimeEnforced,
	)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})).ServeHTTP)

	writer := newDeadlineProbeWriter()
	router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusInternalServerError, writer.status)
	operations := writer.deadlineSnapshot()
	require.Len(t, operations, 2, "bind and reset must enclose panic recovery")
	assert.False(t, operations[0].IsZero())
	assert.True(t, operations[1].IsZero())
}
