// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package middleware

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/control/http/deadline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func shortDeadlineTimeouts() deadline.DeadlineTimeouts {
	return deadline.DeadlineTimeouts{
		APIWriteTimeout:      40 * time.Millisecond,
		MediaWriteTimeout:    120 * time.Millisecond,
		StreamingIdleTimeout: 60 * time.Millisecond,
	}
}

func TestHTTP1KeepAliveDeadlineIsResetBetweenRequests(t *testing.T) {
	handler := enforcedHandler(
		shortDeadlineTimeouts(),
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		}),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	require.NoError(t, err)
	defer conn.Close()
	reader := bufio.NewReader(conn)

	writeRequest := func() {
		_, writeErr := io.WriteString(conn,
			"GET / HTTP/1.1\r\nHost: "+server.Listener.Addr().String()+"\r\nConnection: keep-alive\r\n\r\n",
		)
		require.NoError(t, writeErr)
	}
	readResponse := func() {
		response, readErr := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
		require.NoError(t, readErr)
		body, bodyErr := io.ReadAll(response.Body)
		require.NoError(t, bodyErr)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Equal(t, "ok", string(body))
	}

	writeRequest()
	readResponse()
	time.Sleep(3 * shortDeadlineTimeouts().APIWriteTimeout)
	writeRequest()
	readResponse()
}

func TestHTTP1BoundedDeadlineStopsLateResponse(t *testing.T) {
	handler := enforcedHandler(
		shortDeadlineTimeouts(),
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(4 * shortDeadlineTimeouts().APIWriteTimeout)
			_, _ = io.WriteString(w, "late")
		}),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		return
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(response.Body)
	assert.True(t, readErr != nil || !strings.Contains(string(body), "late"),
		"response written after the fixed deadline unexpectedly reached the client")
}

func TestHTTP2DeadlineIsIsolatedToOneStream(t *testing.T) {
	timeouts := shortDeadlineTimeouts()
	mux := http.NewServeMux()
	slowStarted := make(chan struct{})
	mux.Handle("/slow", WithRoutePolicy(
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		RuntimeEnforced,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(slowStarted)
		time.Sleep(4 * timeouts.APIWriteTimeout)
		_, _ = io.WriteString(w, "late")
	})))
	mux.Handle("/fast", WithRoutePolicy(
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		RuntimeEnforced,
	)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "fast")
	})))

	server := httptest.NewUnstartedServer(WriteTimeoutMiddleware(timeouts, RuntimeEnforced)(mux))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	client.Timeout = time.Second
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.MaxConnsPerHost = 1
	}

	type result struct {
		response *http.Response
		body     []byte
		err      error
	}
	slowResult := make(chan result, 1)
	go func() {
		response, err := client.Get(server.URL + "/slow")
		if err != nil {
			slowResult <- result{err: err}
			return
		}
		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		slowResult <- result{response: response, body: body, err: readErr}
	}()

	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("slow HTTP/2 stream did not start")
	}

	fastResponse, err := client.Get(server.URL + "/fast")
	require.NoError(t, err)
	fastBody, err := io.ReadAll(fastResponse.Body)
	require.NoError(t, err)
	require.NoError(t, fastResponse.Body.Close())
	require.Equal(t, 2, fastResponse.ProtoMajor)
	require.Equal(t, http.StatusOK, fastResponse.StatusCode)
	require.Equal(t, "fast", string(fastBody))

	select {
	case slow := <-slowResult:
		assert.True(t, slow.err != nil || !strings.Contains(string(slow.body), "late"),
			"expired HTTP/2 stream unexpectedly completed")
	case <-time.After(time.Second):
		t.Fatal("expired HTTP/2 stream did not terminate")
	}
}

func TestDeadlineMiddlewareHonorsRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	handler := enforcedHandler(
		deadline.DefaultTimeouts(),
		deadline.RoutePolicy{Class: deadline.RouteDeadlineAPIBounded},
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			called = true
		}),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	assert.True(t, called, "deadline middleware must not replace request cancellation semantics")
}
