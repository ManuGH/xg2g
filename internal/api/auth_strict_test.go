package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/v3/bus"
	"github.com/ManuGH/xg2g/internal/v3/model"
	"github.com/ManuGH/xg2g/internal/v3/store"
)

// ---- SpyStore ----

// ---- SpyStore ----

type SpyStore struct {
	mu         sync.Mutex
	writeCount int64
	leaseCount int64
	leases     map[string]string // key -> owner
	idem       map[string]string // idemKey -> sessionID

	// Configuration
	PanicOnWrite bool // If true, any mutating call panics
}

func (s *SpyStore) IncWrite() {
	if s.PanicOnWrite {
		panic("write operation attempted on reject path")
	}
	atomic.AddInt64(&s.writeCount, 1)
}

func (s *SpyStore) IncLease() { atomic.AddInt64(&s.leaseCount, 1) }

func (s *SpyStore) Writes() int64 { return atomic.LoadInt64(&s.writeCount) }
func (s *SpyStore) Leases() int64 { return atomic.LoadInt64(&s.leaseCount) }

// Implement StateStore interface
func (s *SpyStore) PutSession(ctx context.Context, sr *model.SessionRecord) error {
	s.IncWrite()
	return nil
}
func (s *SpyStore) PutSessionWithIdempotency(ctx context.Context, sr *model.SessionRecord, idemKey string, ttl time.Duration) (string, bool, error) {
	s.IncWrite()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.idem == nil {
		s.idem = make(map[string]string)
	}

	if idemKey != "" {
		if id, ok := s.idem[idemKey]; ok {
			// Simulate existing
			return id, true, nil
		}
		s.idem[idemKey] = sr.SessionID
	}
	return "", false, nil
}
func (s *SpyStore) GetSession(ctx context.Context, id string) (*model.SessionRecord, error) {
	// Return nil, nil is "not found"
	return nil, nil
}
func (s *SpyStore) UpdateSession(ctx context.Context, id string, fn func(*model.SessionRecord) error) (*model.SessionRecord, error) {
	s.IncWrite()
	return nil, nil // No-op
}
func (s *SpyStore) ListSessions(ctx context.Context) ([]*model.SessionRecord, error) {
	return nil, nil
}
func (s *SpyStore) ScanSessions(ctx context.Context, fn func(*model.SessionRecord) error) error {
	return nil
}
func (s *SpyStore) DeleteSession(ctx context.Context, id string) error {
	s.IncWrite()
	return nil
}
func (s *SpyStore) PutPipeline(ctx context.Context, p *model.PipelineRecord) error {
	s.IncWrite()
	return nil
}
func (s *SpyStore) GetPipeline(ctx context.Context, id string) (*model.PipelineRecord, error) {
	return nil, nil
}
func (s *SpyStore) UpdatePipeline(ctx context.Context, id string, fn func(*model.PipelineRecord) error) (*model.PipelineRecord, error) {
	s.IncWrite()
	return nil, nil
}
func (s *SpyStore) PutIdempotency(ctx context.Context, key, sessionID string, ttl time.Duration) error {
	s.IncWrite()
	return nil
}
func (s *SpyStore) GetIdempotency(ctx context.Context, key string) (sessionID string, ok bool, err error) {
	return "", false, nil
}

// Locking Implementation
func (s *SpyStore) TryAcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (store.Lease, bool, error) {
	s.IncLease()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.leases == nil {
		s.leases = make(map[string]string)
	}

	if currentOwner, exists := s.leases[key]; exists {
		if currentOwner == owner {
			return &mockLease{key: key, owner: owner}, true, nil // Re-entrant
		}
		return nil, false, nil // Busy
	}

	s.leases[key] = owner
	return &mockLease{key: key, owner: owner}, true, nil
}

func (s *SpyStore) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (store.Lease, bool, error) {
	s.IncLease()
	s.mu.Lock()
	defer s.mu.Unlock()

	if currentOwner, exists := s.leases[key]; exists && currentOwner == owner {
		return &mockLease{key: key, owner: owner}, true, nil
	}
	return nil, false, nil
}

func (s *SpyStore) ReleaseLease(ctx context.Context, key, owner string) error {
	s.IncLease()
	s.mu.Lock()
	defer s.mu.Unlock()

	if currentOwner, exists := s.leases[key]; exists && currentOwner == owner {
		delete(s.leases, key)
	}
	return nil
}

func (s *SpyStore) DeleteAllLeases(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := len(s.leases)
	s.leases = make(map[string]string)
	// Do NOT call s.IncLease() here to avoid polluting request-scoped counters
	return count, nil
}

type mockLease struct {
	key, owner string
}

func (l *mockLease) Key() string          { return l.key }
func (l *mockLease) Owner() string        { return l.owner }
func (l *mockLease) ExpiresAt() time.Time { return time.Now().Add(1 * time.Minute) }

// ---- Test Harness ----

type testServer struct {
	ts  *httptest.Server
	url string
}

type testTokens struct {
	admin string
	read  string
	write string
}

func newTestServer(t *testing.T, spy *SpyStore) (testServer, testTokens) {
	// Re-use parameterized constructor with default config
	// TrustedProxies must TRUST the test runner (127.0.0.1) but NOT the public IP (8.8.8.8)
	// to allow XFF spoofing tests to work correctly.
	return newTestServerConfig(t, spy, nil, func(cfg *config.AppConfig) {
		cfg.TrustedProxies = "127.0.0.1,::1"
	})
}

func TestLANGuard_UntrustedProxy(t *testing.T) {
	spy := &SpyStore{}

	// Case 1: TrustedProxies IS configured (127.0.0.1).
	// The test runner (RemoteAddr) IS trusted.
	// We send XFF="8.8.8.8".
	// Expectation: Middleware sees 8.8.8.8 -> REJECT (403).
	t.Run("trusted_proxy_honors_xff", func(t *testing.T) {
		srv, _ := newTestServerConfig(t, spy, nil, func(cfg *config.AppConfig) {
			cfg.TrustedProxies = "127.0.0.1"
		})
		client := srv.ts.Client()

		// XFF = Public IP
		resp := doReq(t, client, http.MethodGet, srv.url+"/xmltv.xml", nil, "", "8.8.8.8")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 (XFF honored), got %d", resp.StatusCode)
		}
	})

	// Case 2: TrustedProxies is EMPTY/Missing.
	t.Run("untrusted_proxy_ignores_xff", func(t *testing.T) {
		srv, _ := newTestServerConfig(t, spy, nil, func(cfg *config.AppConfig) {
			cfg.TrustedProxies = "" // No proxies trusted
		})
		client := srv.ts.Client()

		// XFF = Public IP
		resp := doReq(t, client, http.MethodGet, srv.url+"/xmltv.xml", nil, "", "8.8.8.8")
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusForbidden {
			t.Errorf("expected NOT 403 (XFF ignored, Localhost used), got %d", resp.StatusCode)
		}
	})

	// Case 3: Chained Proxies (Right-to-Left Stripping).
	// Config: Trusted = 127.0.0.1, 10.0.0.0/8
	// Request: XFF="8.8.8.8, 10.0.0.5"
	// RemoteAddr: 127.0.0.1 (Trusted)
	// Logic:
	// - 127.0.0.1 Trusted? Yes. Check XFF.
	// - Parse XFF: ["8.8.8.8", "10.0.0.5"]
	// - Rightmost: "10.0.0.5". Is trusted (10.0.0.0/8)? Yes. Skip.
	// - Next: "8.8.8.8". Is trusted? No. -> Client IP.
	// - Check 8.8.8.8 against AllowedClients (LAN)? No.
	// - Result: 403 Forbidden.
	t.Run("chained_proxy_right_to_left", func(t *testing.T) {
		srv, _ := newTestServerConfig(t, spy, nil, func(cfg *config.AppConfig) {
			cfg.TrustedProxies = "127.0.0.1,10.0.0.0/8" // Trust localhost AND 10.x upstream
		})
		client := srv.ts.Client()

		// XFF = "Public, TrustedInternal"
		resp := doReq(t, client, http.MethodGet, srv.url+"/xmltv.xml", nil, "", "8.8.8.8, 10.0.0.5")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 (8.8.8.8 resolved as client), got %d", resp.StatusCode)
		}
	})
}

// ---- SpyBus ----

type SpyBus struct {
	mu     sync.Mutex
	events []bus.Message
}

func (b *SpyBus) Publish(ctx context.Context, topic string, msg bus.Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, msg)
	return nil
}

func (b *SpyBus) Subscribe(ctx context.Context, topic string) (bus.Subscriber, error) {
	// Dummy subscriber for tests if needed, or return error if not used
	return &mockSubscriber{}, nil
}

func (b *SpyBus) Events() []bus.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Return copy
	return append([]bus.Message(nil), b.events...)
}

type mockSubscriber struct{}

func (s *mockSubscriber) C() <-chan bus.Message { return make(chan bus.Message) }
func (s *mockSubscriber) Close() error          { return nil }

// Updated signature to accept optional SpyBus (nil = create new)
func newTestServerConfig(t *testing.T, spy *SpyStore, spyBus *SpyBus, fn func(*config.AppConfig)) (testServer, testTokens) {
	t.Helper()

	cfg := config.AppConfig{}
	// defaults
	cfg.DataDir = t.TempDir()
	cfg.LogLevel = "error"
	cfg.OWITimeout = 1 * time.Second
	cfg.ConfigStrict = true
	cfg.EPGEnabled = false
	cfg.RateLimitEnabled = false
	cfg.TunerSlots = []int{0}
	cfg.V3APILeases = true // Default Phase 1

	_ = os.MkdirAll(cfg.DataDir+"/picons", 0755)

	cfg.APIToken = ""
	cfg.APITokens = []config.ScopedToken{
		{Token: "token-admin", Scopes: []string{"v3:admin"}},
		{Token: "token-read", Scopes: []string{"v3:read"}},
		{Token: "token-write", Scopes: []string{"v3:write"}},
	}
	cfg.AuthAnonymous = false

	// Apply custom overrides
	if fn != nil {
		fn(&cfg)
	}

	cfgMgr := config.NewManager(cfg.DataDir + "/config.yaml")

	srv := New(cfg, cfgMgr)
	var b bus.Bus
	if spyBus != nil {
		b = spyBus
	} else {
		b = bus.NewMemoryBus()
	}
	srv.SetV3Components(b, spy)

	handler := srv.routes()

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return testServer{ts: ts, url: ts.URL}, testTokens{
		admin: "token-admin",
		read:  "token-read",
		write: "token-write",
	}
}

func doReq(t *testing.T, client *http.Client, method, url string, body any, token string, remoteAddr string) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if remoteAddr != "" {
		req.Header.Set("X-Forwarded-For", remoteAddr)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// ---- Tests ----

func TestAuthEnforcement_TableDriven(t *testing.T) {
	spy := &SpyStore{}
	srv, tok := newTestServer(t, spy)
	client := srv.ts.Client()

	type tc struct {
		name         string
		method       string
		path         string
		token        string
		wantStatus   int
		wantNoWrites bool
		wantNoLeases bool
	}

	cases := []tc{
		{
			name:         "control plane start intent no token -> 401",
			method:       http.MethodPost,
			path:         "/api/v3/intents",
			token:        "",
			wantStatus:   http.StatusUnauthorized,
			wantNoWrites: true,
			wantNoLeases: true,
		},
		{
			name:         "control plane start intent read token -> 403",
			method:       http.MethodPost,
			path:         "/api/v3/intents",
			token:        tok.read,
			wantStatus:   http.StatusForbidden,
			wantNoWrites: true,
			wantNoLeases: true,
		},

		// HLS parity checks (playlist + segment):
		{
			name:   "hls playlist no token -> 401",
			method: http.MethodGet,
			// Use valid UUID to pass router validation
			path:         "/api/v3/sessions/00000000-0000-0000-0000-000000000000/hls/index.m3u8",
			token:        "",
			wantStatus:   http.StatusUnauthorized,
			wantNoWrites: true,
			wantNoLeases: true,
		},
		{
			name:         "hls segment no token -> 401",
			method:       http.MethodGet,
			path:         "/api/v3/sessions/00000000-0000-0000-0000-000000000000/hls/seg-00001.ts",
			token:        "",
			wantStatus:   http.StatusUnauthorized, // Should be 401 even if file doesn't exist
			wantNoWrites: true,
			wantNoLeases: true,
		},

		// Observability/Internal not exposed:
		{
			name:         "metrics not exposed",
			method:       http.MethodGet,
			path:         "/metrics",
			token:        "",
			wantStatus:   http.StatusNotFound, // Assuming 404 since route not registered
			wantNoWrites: true,
			wantNoLeases: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Strict Mode: any write during a reject path causes panic/fail
			spy.PanicOnWrite = c.wantNoWrites

			beforeW := spy.Writes()
			beforeL := spy.Leases()

			// Dummy body for intents
			var body any
			if c.method == http.MethodPost {
				body = map[string]string{
					"type":       "stream.start",
					"serviceRef": "1:0:1:...",
				}
			}

			resp := doReq(t, client, c.method, srv.url+c.path, body, c.token, "")
			defer resp.Body.Close()

			if resp.StatusCode != c.wantStatus {
				t.Errorf("expected %d, got %d", c.wantStatus, resp.StatusCode)
			}

			// Disable strict mode for cleanup/assertions
			spy.PanicOnWrite = false

			afterW := spy.Writes()
			afterL := spy.Leases()

			if c.wantNoWrites && afterW != beforeW {
				t.Errorf("reject must be side-effect free (writes): before=%d after=%d", beforeW, afterW)
			}
			if c.wantNoLeases && afterL != beforeL {
				t.Errorf("reject must be side-effect free (leases): before=%d after=%d", beforeL, afterL)
			}
		})
	}
}

func TestLANGuard_PublicEndpoints(t *testing.T) {
	spy := &SpyStore{PanicOnWrite: true} // Zero writes allowed
	srv, _ := newTestServer(t, spy)
	client := srv.ts.Client()

	paths := []string{
		"/xmltv.xml",
		"/playlist.m3u",
	}

	for _, p := range paths {
		t.Run("public_ip_forbidden_"+p, func(t *testing.T) {
			// Simulate Public IP
			resp := doReq(t, client, http.MethodGet, srv.url+p, nil, "", "8.8.8.8")
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("expected 403 for public IP, got %d", resp.StatusCode)
			}
		})

		t.Run("lan_ip_allowed_"+p, func(t *testing.T) {
			// 10.x should be allowed
			// Note: Response might be 404 (file missing) but NOT 403
			resp := doReq(t, client, http.MethodGet, srv.url+p, nil, "", "10.10.55.123")
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusForbidden {
				t.Errorf("expected not-403 for LAN IP, got %d", resp.StatusCode)
			}
		})

	}
}

func TestRaceSafety_ParallelIntents(t *testing.T) {
	// Phase 1: Legacy (API Leases ON)
	t.Run("Phase1_Legacy_API_Locking", func(t *testing.T) {
		spy := &SpyStore{}
		spyBus := &SpyBus{}
		srv, tok := newTestServerConfig(t, spy, spyBus, func(cfg *config.AppConfig) {
			cfg.V3APILeases = true
		})
		runRaceTest(t, srv, tok, spy, spyBus, true)
	})

	// Phase 2: Worker Logic (API Leases OFF)
	// Currently this will FAIL because implementation is missing (Store won't see API skip).
	// But the test structure is ready.
	t.Run("Phase2_Worker_Dedup", func(t *testing.T) {
		spy := &SpyStore{}
		spyBus := &SpyBus{}
		srv, tok := newTestServerConfig(t, spy, spyBus, func(cfg *config.AppConfig) {
			cfg.V3APILeases = false
		})

		// In Phase 2, we expect 2 Accepted (Dedup happens at Worker or Idempotency check).
		// Since Idempotency isn't implemented yet, we essentially test that API strictly returns 202
		// without checking locks.
		runRaceTest(t, srv, tok, spy, spyBus, false)
	})
}

func runRaceTest(t *testing.T, srv testServer, tok testTokens, spy *SpyStore, spyBus *SpyBus, phase1 bool) {
	client := srv.ts.Client()

	const n = 2
	var wg sync.WaitGroup
	wg.Add(n)

	// Capture SessionIDs
	sessionIDs := make([]string, n)
	statuses := make([]int, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			// Use same serviceRef to trigger collision
			body := map[string]any{
				"type":       "stream.start",
				"profileID":  "auto",
				"serviceRef": "1:0:19:132F:3EF:1:C00000:0:0:0:RACETEST",
			}
			resp := doReq(t, client, http.MethodPost, srv.url+"/api/v3/intents", body, tok.write, "127.0.0.1")
			defer resp.Body.Close()
			statuses[idx] = resp.StatusCode

			// Parse response for SessionID
			var respMap map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&respMap); err == nil {
				if sid, ok := respMap["sessionId"].(string); ok {
					sessionIDs[idx] = sid
				}
			}
		}(i)
	}
	wg.Wait()

	accepted := 0
	conflict := 0
	other := 0

	for _, st := range statuses {
		switch st {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		default:
			other++
		}
	}

	if phase1 {
		// Phase 1: 1 Winner (202), 1 Loser (409)
		if accepted != 1 {
			t.Errorf("[Phase1] expected exactly 1 accepted, got %d (statuses: %v)", accepted, statuses)
		}
		if conflict != 1 {
			t.Errorf("[Phase1] expected exactly 1 conflict, got %d (statuses: %v)", conflict, statuses)
		}
	} else {
		// Phase 2: 2 Accepted (202). No Conflicts from API.
		// Worker handles dedup downstream.
		if accepted != 2 {
			t.Errorf("[Phase2] expected 2 accepted (API non-blocking), got %d (statuses: %v)", accepted, statuses)
		}
		// Logic Check: SpyBus should see 1 event (because Atomic Idempotency suppressed the second one).
		events := len(spyBus.Events())
		if events != 1 {
			t.Errorf("[Phase2] expected 1 bus events (atomic suppression), got %d", events)
		}

		// Confirm duplicate requests got SAME session ID (Idempotency)
		if sessionIDs[0] == "" || sessionIDs[1] == "" {
			t.Errorf("[Phase2] Failed to parse session IDs: %v", sessionIDs)
		} else if sessionIDs[0] != sessionIDs[1] {
			t.Errorf("[Phase2] Idempotency Failed. Expected identical sessionIDs, got: %s vs %s", sessionIDs[0], sessionIDs[1])
		}
	}

	if other > 0 {
		t.Errorf("unexpected statuses: %v", statuses)
	}
}
