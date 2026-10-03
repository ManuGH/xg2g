// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/iptv/edge"
	"github.com/ManuGH/xg2g/internal/iptv/sourceref"
	platformnet "github.com/ManuGH/xg2g/internal/platform/net"
	"github.com/ManuGH/xg2g/internal/receivertopology"
	"github.com/ManuGH/xg2g/internal/stream/ingest/ring"
	"github.com/ManuGH/xg2g/internal/stream/ingest/session"
	"github.com/ManuGH/xg2g/internal/stream/ingest/tsfixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodeIPTVURL(rawURL string) string {
	return strings.ReplaceAll(rawURL, ":", "%3a")
}

type trackingTopologyService struct {
	mu           sync.Mutex
	reserveCalls []string
	releaseCalls []string
	allow        bool
}

func newTrackingTopologyService(allow bool) *trackingTopologyService {
	return &trackingTopologyService{allow: allow}
}

func (s *trackingTopologyService) ReserveStreamLeaseAtomic(serviceRef string, sessionID string, priority receivertopology.Priority, ttl time.Duration) (*receivertopology.Lease, receivertopology.AllocationDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reserveCalls = append(s.reserveCalls, serviceRef)
	return &receivertopology.Lease{}, receivertopology.AllocationDecision{Allowed: s.allow}, nil
}

func (s *trackingTopologyService) ReleaseStream(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseCalls = append(s.releaseCalls, sessionID)
	return true
}

func (s *trackingTopologyService) ReserveCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reserveCalls)
}

func (s *trackingTopologyService) ReleaseCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.releaseCalls)
}

func testAllowPolicyForURL(t *testing.T, rawURL string) platformnet.OutboundPolicy {
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	port := 80
	if u.Port() != "" {
		p, err := strconv.Atoi(u.Port())
		require.NoError(t, err)
		port = p
	} else if u.Scheme == "https" {
		port = 443
	}
	return platformnet.OutboundPolicy{
		Enabled: true,
		Allow: platformnet.OutboundAllowlist{
			Hosts:   []string{u.Hostname()},
			CIDRs:   []string{"127.0.0.1/32"},
			Ports:   []int{port},
			Schemes: []string{u.Scheme},
		},
	}
}

func writeTestTSPackets(w io.Writer, count int) {
	pkt := make([]byte, ring.TSPacketSize)
	pkt[0] = ring.SyncByte
	chunk := make([]byte, count*ring.TSPacketSize)
	for i := 0; i < len(chunk); i += ring.TSPacketSize {
		copy(chunk[i:], pkt)
	}
	_, _ = w.Write(chunk)
}

// 3.a: DVB reference with colliding service-ID attributes still routing to the receiver dialer and holding a tuner lease
func TestConnector_IPTV_SourceSelection_DVB_CollidingServiceID(t *testing.T) {
	collidingRef := "1:0:1:4E27:43A:1:C00000:0:0:0:" // PULS 4 Austria SD (DVB-S2 satellite transponder)
	topo := newTrackingTopologyService(true)

	var dialedKey session.SessionKey
	var dialCalled atomic.Bool

	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.TopologyService = topo
	cfg.RequireTopology = true
	cfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		dialCalled.Store(true)
		dialedKey = key
		pr, pw := io.Pipe()
		go func() {
			writeTestTSPackets(pw, 20)
			_ = pw.Close()
		}()
		return pr, nil
	}

	connector := NewLivePipelineConnector(cfg)
	key := session.NewSessionKey("127.0.0.1", 8001, collidingRef)

	wrapper, err := connector.Connect(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = wrapper.Close() }()

	assert.True(t, dialCalled.Load(), "receiver dialer must be called for DVB reference")
	assert.Equal(t, collidingRef, dialedKey.ServiceRef)
	assert.Equal(t, 1, topo.ReserveCallCount(), "tuner lease must be reserved for DVB reference")

	streamWrapper, ok := wrapper.(*PipelineStreamWrapper)
	require.True(t, ok)
	assert.NotNil(t, streamWrapper.TopologyLease(), "TopologyLease must be non-nil on wrapper")

	_ = wrapper.Close()
	assert.Equal(t, 1, topo.ReleaseCallCount(), "tuner lease must be released when wrapper is closed")
}

// 3.b: Pure IPTV reference routing to the provider ingest path with zero tuner leases
func TestConnector_IPTV_SourceSelection_PureIPTV_ZeroTunerLease(t *testing.T) {
	var providerHit atomic.Bool
	var receivedUA string

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerHit.Store(true)
		receivedUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		writeTestTSPackets(w, 50)
	}))
	defer providerSrv.Close()

	topo := newTrackingTopologyService(true)
	var dvbDialCalled atomic.Bool

	encodedURL := encodeIPTVURL(providerSrv.URL + "/stream.ts")
	iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Canary", encodedURL)

	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.TopologyService = topo
	cfg.RequireTopology = true
	cfg.OutboundPolicy = testAllowPolicyForURL(t, providerSrv.URL)
	cfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		dvbDialCalled.Store(true)
		return nil, fmt.Errorf("receiver dialer should not be called for IPTV")
	}

	connector := NewLivePipelineConnector(cfg)
	key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

	wrapper, err := connector.Connect(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = wrapper.Close() }()

	assert.True(t, providerHit.Load(), "provider HTTP endpoint must be dialed for IPTV reference")
	assert.False(t, dvbDialCalled.Load(), "DVB dialer must never be called for IPTV reference")
	assert.Equal(t, 0, topo.ReserveCallCount(), "Topology lease must NOT be requested for IPTV reference (zero leases)")
	assert.Equal(t, "curl/8.10.1", receivedUA, "User-Agent must be curl/8.10.1")

	streamWrapper, ok := wrapper.(*PipelineStreamWrapper)
	require.True(t, ok)
	assert.Nil(t, streamWrapper.TopologyLease(), "TopologyLease must be nil for IPTV wrapper")
}

// 3.c: Provider redirects handled cleanly
func TestConnector_IPTV_ProviderRedirect_HandledCleanly(t *testing.T) {
	var redirectedHit atomic.Bool

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/edge/initial.ts" {
			http.Redirect(w, r, "/node/destination.ts", http.StatusFound)
			return
		}
		if r.URL.Path == "/node/destination.ts" {
			redirectedHit.Store(true)
			w.Header().Set("Content-Type", "video/mp2t")
			w.WriteHeader(http.StatusOK)
			writeTestTSPackets(w, 50)
			return
		}
		http.NotFound(w, r)
	}))
	defer providerSrv.Close()

	encodedURL := encodeIPTVURL(providerSrv.URL + "/edge/initial.ts")
	iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.OutboundPolicy = testAllowPolicyForURL(t, providerSrv.URL)

	connector := NewLivePipelineConnector(cfg)
	key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

	wrapper, err := connector.Connect(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = wrapper.Close() }()

	assert.True(t, redirectedHit.Load(), "redirected destination must be reached and streamed")
}

// 3.d: Blocked redirect targets rejected without leaking destination or reading media bytes
func TestConnector_IPTV_BlockedRedirect_RejectedWithoutLeakOrMediaBytes(t *testing.T) {
	var secretDestinationHit atomic.Bool
	var secretBytesRead atomic.Int64

	// Blocked secret destination server
	secretSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secretDestinationHit.Store(true)
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		data := make([]byte, 1000)
		n, _ := w.Write(data)
		secretBytesRead.Add(int64(n))
	}))
	defer secretSrv.Close()

	secretURL := secretSrv.URL + "/super-secret-token-path-do-not-leak"

	// Edge server that attempts to redirect to the blocked secret server
	edgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secretURL, http.StatusFound)
	}))
	defer edgeSrv.Close()

	encodedURL := encodeIPTVURL(edgeSrv.URL + "/initial.ts")
	iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	// Outbound policy only allows edgeSrv, NOT secretSrv!
	cfg.OutboundPolicy = testAllowPolicyForURL(t, edgeSrv.URL)

	connector := NewLivePipelineConnector(cfg)
	key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

	wrapper, err := connector.Connect(context.Background(), key)
	assert.Error(t, err, "connect must fail closed when redirect target is blocked")
	assert.Nil(t, wrapper, "no wrapper created on blocked redirect")

	// Verification of security requirements:
	assert.False(t, secretDestinationHit.Load(), "blocked redirect destination must never be reached")
	assert.Equal(t, int64(0), secretBytesRead.Load(), "zero media bytes must be read from blocked target")
	assert.NotContains(t, err.Error(), "super-secret-token-path-do-not-leak", "error message must not leak redirect destination path")
	assert.NotContains(t, err.Error(), secretSrv.URL, "error message must not leak redirect destination URL")
}

// 3.e: Non-200 / HTML / error responses failing closed
func TestConnector_IPTV_ErrorResponses_FailClosed(t *testing.T) {
	t.Run("HTTP 404 Not Found fails closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer srv.Close()

		encodedURL := encodeIPTVURL(srv.URL + "/not_found.ts")
		iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

		cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
		cfg.OutboundPolicy = testAllowPolicyForURL(t, srv.URL)

		connector := NewLivePipelineConnector(cfg)
		key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

		wrapper, err := connector.Connect(context.Background(), key)
		assert.Error(t, err)
		assert.Nil(t, wrapper)
		assert.Contains(t, err.Error(), "HTTP 404")
	})

	t.Run("HTTP 500 Internal Server Error fails closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "server error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		encodedURL := encodeIPTVURL(srv.URL + "/error.ts")
		iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

		cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
		cfg.OutboundPolicy = testAllowPolicyForURL(t, srv.URL)

		connector := NewLivePipelineConnector(cfg)
		key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

		wrapper, err := connector.Connect(context.Background(), key)
		assert.Error(t, err)
		assert.Nil(t, wrapper)
		assert.Contains(t, err.Error(), "HTTP 500")
	})

	t.Run("HTTP 200 with text/html content-type fails closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>Portal Login / Captive Portal</body></html>"))
		}))
		defer srv.Close()

		encodedURL := encodeIPTVURL(srv.URL + "/portal.html")
		iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

		cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
		cfg.OutboundPolicy = testAllowPolicyForURL(t, srv.URL)

		connector := NewLivePipelineConnector(cfg)
		key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

		wrapper, err := connector.Connect(context.Background(), key)
		assert.Error(t, err)
		assert.Nil(t, wrapper)
		assert.Contains(t, err.Error(), "non-stream content-type")
	})
}

// 3.f: Two subscribers sharing one upstream stream without a second dial
func TestConnector_IPTV_TwoSubscribers_SharedUpstream_SingleDial(t *testing.T) {
	var dialCount atomic.Int32

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dialCount.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				writeTestTSPackets(w, 20)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	}))
	defer providerSrv.Close()

	encodedURL := encodeIPTVURL(providerSrv.URL + "/live/stream.ts")
	iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.OutboundPolicy = testAllowPolicyForURL(t, providerSrv.URL)

	connector := NewLivePipelineConnector(cfg)
	mgr := session.NewManager(session.DefaultManagerConfig(), connector)
	defer mgr.Close()

	key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Subscriber 1 acquires
	lease1, err := mgr.Acquire(ctx, key)
	require.NoError(t, err)
	defer lease1.Release()

	assert.Equal(t, int32(1), dialCount.Load(), "first subscriber triggers exactly 1 upstream dial")

	// Subscriber 2 acquires the same channel
	lease2, err := mgr.Acquire(ctx, key)
	require.NoError(t, err)
	defer lease2.Release()

	assert.Equal(t, int32(1), dialCount.Load(), "second subscriber reuses existing upstream without second dial")
}

// 3.g: Final subscriber stop closing the upstream connection
func TestConnector_IPTV_FinalSubscriberStop_ClosesUpstreamConnection(t *testing.T) {
	connStarted := make(chan struct{})
	connClosed := make(chan struct{})
	var closeOnce sync.Once

	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		closeOnce.Do(func() { close(connStarted) })
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)

		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				close(connClosed)
				return
			case <-ticker.C:
				writeTestTSPackets(w, 20)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	}))
	defer providerSrv.Close()

	encodedURL := encodeIPTVURL(providerSrv.URL + "/live/stream.ts")
	iptvRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Channel", encodedURL)

	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.OutboundPolicy = testAllowPolicyForURL(t, providerSrv.URL)

	connector := NewLivePipelineConnector(cfg)
	// Zero warm-hold so final subscriber release immediately terminates the upstream session
	mgrCfg := session.DefaultManagerConfig()
	mgrCfg.WarmHoldDuration = 0
	mgr := session.NewManager(mgrCfg, connector)
	defer mgr.Close()

	key := session.NewSessionKey("127.0.0.1", 8001, iptvRef)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	lease1, err := mgr.Acquire(ctx, key)
	require.NoError(t, err)

	select {
	case <-connStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream connection did not start")
	}

	lease2, err := mgr.Acquire(ctx, key)
	require.NoError(t, err)

	// Subscriber 1 leaves: connection must stay open because subscriber 2 is still active
	lease1.Release()

	select {
	case <-connClosed:
		t.Fatal("upstream connection closed prematurely while subscriber 2 was still active")
	case <-time.After(100 * time.Millisecond):
		// Connection remains open as expected
	}

	// Subscriber 2 leaves: final subscriber stop must close upstream connection
	lease2.Release()

	select {
	case <-connClosed:
		// Upstream HTTP connection closed cleanly!
	case <-time.After(3 * time.Second):
		t.Fatal("upstream connection did not close after final subscriber released")
	}
}

// 3.h: Ordinary DVB streaming remaining intact
func TestConnector_IPTV_OrdinaryDVBStreaming_Intact(t *testing.T) {
	dvbRef := "1:0:19:283D:3FB:1:C00000:0:0:0:" // Das Erste HD (DVB)
	topo := newTrackingTopologyService(true)

	var dialCount atomic.Int32
	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.TopologyService = topo
	cfg.RequireTopology = true
	cfg.DialFn = func(ctx context.Context, key session.SessionKey) (io.ReadCloser, error) {
		dialCount.Add(1)
		pr, pw := io.Pipe()
		go func() {
			writeTestTSPackets(pw, 20)
			_ = pw.Close()
		}()
		return pr, nil
	}

	connector := NewLivePipelineConnector(cfg)
	key := session.NewSessionKey("127.0.0.1", 8001, dvbRef)

	wrapper, err := connector.Connect(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = wrapper.Close() }()

	assert.Equal(t, int32(1), dialCount.Load(), "receiver dialer must be invoked for DVB stream")
	assert.Equal(t, 1, topo.ReserveCallCount(), "tuner lease must be acquired for DVB stream")

	streamWrapper, ok := wrapper.(*PipelineStreamWrapper)
	require.True(t, ok)
	assert.NotNil(t, streamWrapper.TopologyLease(), "TopologyLease must be held for DVB stream")

	_ = wrapper.Close()
	assert.Equal(t, 1, topo.ReleaseCallCount(), "tuner lease must be released on stream close")
}

// 4. Gate 2 End-to-End Proof:
// authenticated request with iptv_<id> -> resolved reference -> production IPTV provider ingest
// -> primed ring-buffer attach -> nonempty MPEG-TS output -> verified audio/video decode -> clean stop.
func TestConnector_IPTV_Gate2_EndToEnd_OpaqueID_To_DecodedMedia(t *testing.T) {
	fixtureData := tsfixture.Load(t, "verify_final_v3.ts")
	require.NotEmpty(t, fixtureData, "test capture verify_final_v3.ts must be available")

	// 1. Setup mock IPTV provider serving the real MPEG-TS fixture in a loop with realistic pacing
	var providerHits atomic.Int32
	providerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerHits.Add(1)
		w.Header().Set("Content-Type", "video/mp2t")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		const chunkSize = 188 * 100 // 18,800 bytes
		offset := 0
		for {
			select {
			case <-r.Context().Done():
				return
			default:
				end := offset + chunkSize
				if end > len(fixtureData) {
					end = len(fixtureData)
				}
				chunk := fixtureData[offset:end]
				offset = end
				if offset >= len(fixtureData) {
					offset = 0 // loop fixture
				}

				if _, err := w.Write(chunk); err != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				// 18.8 KB every 15ms is ~10 Mbps, matching the ~9 Mbps TS bitrate
				time.Sleep(15 * time.Millisecond)
			}
		}
	}))
	defer providerSrv.Close()

	// 2. Setup sourceref parser, registry, and edge resolver
	secret := "gate2-secret-key-at-least-32-bytes!!"
	parser, err := sourceref.NewParser([]byte(secret))
	require.NoError(t, err)

	encodedURL := encodeIPTVURL(providerSrv.URL + "/live/stream.ts")
	rawRef := fmt.Sprintf("4097:0:1:0:0:0:0:0:0:0:%s:Gate2Channel", encodedURL)

	src, err := parser.Parse(rawRef)
	require.NoError(t, err)

	reg := sourceref.NewRegistry()
	require.NoError(t, reg.Replace([]sourceref.Source{src}))

	resolver := edge.NewResolver(reg, parser, nil)
	opaqueID := string(src.ID())
	require.True(t, strings.HasPrefix(opaqueID, "iptv_"))

	// 3. Setup production pipeline connector and session manager
	topo := newTrackingTopologyService(true)
	cfg := DefaultTestConnectorConfig("127.0.0.1", 8001)
	cfg.IPTVParser = parser
	cfg.TopologyService = topo
	cfg.RequireTopology = true
	cfg.OutboundPolicy = testAllowPolicyForURL(t, providerSrv.URL)

	connector := NewLivePipelineConnector(cfg)
	mgr := session.NewManager(session.DefaultManagerConfig(), connector)
	defer mgr.Close()

	// 4. Setup edge HTTP handler with resolver
	handler := NewHandlerWithReceiver(mgr, "127.0.0.1", 8001)
	handler.SetIPTVResolver(resolver)

	// 5. Authenticated HTTP request to /api/v3/stream/live/{opaqueID}
	clientCtx, clientCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer clientCancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v3/stream/live/"+opaqueID, nil).WithContext(clientCtx)
	w := newThreadSafeStreamRecorder()

	// Run handler in background goroutine since it streams continuously
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		handler.ServeHTTP(w, req)
	}()

	// Read bytes from recorder until we have at least 500 KB (enough for ffprobe keyframe decode)
	deadline := time.Now().Add(5 * time.Second)
	for w.Len() < 500*1024 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	capturedBytes := w.Bytes()
	require.GreaterOrEqual(t, len(capturedBytes), 100*1024, "must have received at least 100 KB of MPEG-TS stream data")

	// 6. Verify zero tuner leases were requested from physical tuner topology!
	assert.Equal(t, 0, topo.ReserveCallCount(), "IPTV playback must have ZERO tuner leases on physical topology")
	assert.Equal(t, int32(1), providerHits.Load(), "provider must have been dialed exactly once")

	// 7. Verify ffprobe video decode on the received stream bytes
	ffprobeVideoCmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height", "-of", "csv=p=0", "pipe:0")
	ffprobeVideoCmd.Stdin = bytes.NewReader(capturedBytes)
	videoOut, err := ffprobeVideoCmd.Output()
	require.NoError(t, err, "ffprobe video probe must succeed on captured IPTV stream")
	assert.Contains(t, string(videoOut), "h264", "decoded video codec must be h264")
	assert.Contains(t, string(videoOut), "1280", "decoded video width must be 1280")
	assert.Contains(t, string(videoOut), "720", "decoded video height must be 720")

	// 8. Verify ffprobe audio decode on the received stream bytes
	ffprobeAudioCmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", "pipe:0")
	ffprobeAudioCmd.Stdin = bytes.NewReader(capturedBytes)
	audioOut, err := ffprobeAudioCmd.Output()
	require.NoError(t, err, "ffprobe audio probe must succeed on captured IPTV stream")
	assert.Contains(t, string(audioOut), "aac", "decoded audio codec must be aac")

	// 9. Clean stop: cancel client request and verify clean termination
	clientCancel()
	select {
	case <-handlerDone:
		// Handler returned cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not terminate cleanly after client cancel")
	}
}

type threadSafeStreamRecorder struct {
	mu         sync.Mutex
	header     http.Header
	buf        bytes.Buffer
	statusCode int
}

func newThreadSafeStreamRecorder() *threadSafeStreamRecorder {
	return &threadSafeStreamRecorder{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (r *threadSafeStreamRecorder) Header() http.Header {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.header
}

func (r *threadSafeStreamRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *threadSafeStreamRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusCode = code
}

func (r *threadSafeStreamRecorder) Flush() {}

func (r *threadSafeStreamRecorder) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := make([]byte, r.buf.Len())
	copy(b, r.buf.Bytes())
	return b
}

func (r *threadSafeStreamRecorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}
