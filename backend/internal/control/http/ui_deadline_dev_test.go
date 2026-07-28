//go:build dev

// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package http_test

import (
	"bufio"
	"io"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	controlhttp "github.com/ManuGH/xg2g/internal/control/http"
	"github.com/ManuGH/xg2g/internal/control/http/deadline"
	"github.com/ManuGH/xg2g/internal/control/middleware"
	"github.com/stretchr/testify/require"
)

func TestDevProxyUpgradeClearsRouteDeadlineAfterSuccessfulHijack(t *testing.T) {
	t.Setenv("XG2G_UI_DEV_DIR", "")
	t.Setenv("XG2G_UI_DEV_PROXY_URL", "")
	upstreamPath := make(chan string, 1)
	upstream := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		upstreamPath <- r.URL.Path
		conn, rw, err := w.(nethttp.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(rw,
			"HTTP/1.1 101 Switching Protocols\r\n"+
				"Connection: Upgrade\r\n"+
				"Upgrade: websocket\r\n\r\n",
		)
		_ = rw.Flush()
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		_, _ = rw.WriteString(line)
		_ = rw.Flush()
	}))
	defer upstream.Close()

	timeouts := deadline.DeadlineTimeouts{
		APIWriteTimeout:      30 * time.Millisecond,
		MediaWriteTimeout:    60 * time.Millisecond,
		StreamingIdleTimeout: 40 * time.Millisecond,
	}
	policy := deadline.RoutePolicy{
		Class:                deadline.RouteDeadlineMediaBounded,
		MayUpgradePerRequest: true,
	}
	proxy := controlhttp.UIHandler(controlhttp.UIConfig{DevProxyURL: upstream.URL})
	handler := middleware.WriteTimeoutMiddleware(timeouts, middleware.RuntimeEnforced)(
		middleware.Compression()(
			middleware.WithRoutePolicy(policy, middleware.RuntimeEnforced)(proxy),
		),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	conn, err := net.DialTimeout("tcp", serverURL.Host, time.Second)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	_, err = io.WriteString(conn,
		"GET /socket HTTP/1.1\r\n"+
			"Host: "+serverURL.Host+"\r\n"+
			"Connection: Upgrade\r\n"+
			"Upgrade: websocket\r\n\r\n",
	)
	require.NoError(t, err)

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, statusLine, "101 Switching Protocols")
	for {
		line, readErr := reader.ReadString('\n')
		require.NoError(t, readErr)
		if line == "\r\n" {
			break
		}
	}

	select {
	case path := <-upstreamPath:
		require.Equal(t, "/ui/socket", path)
	case <-time.After(time.Second):
		t.Fatal("upgrade did not reach DevProxy upstream")
	}

	// Wait beyond the original bounded Media deadline. The upgraded tunnel must
	// remain writable because the successful HTTP/1 hijack cleared it.
	time.Sleep(3 * timeouts.MediaWriteTimeout)
	_, err = io.WriteString(conn, "ping\n")
	require.NoError(t, err)
	echo, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ping", strings.TrimSpace(echo))
}
