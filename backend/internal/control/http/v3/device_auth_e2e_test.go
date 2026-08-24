package v3

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/http/v3/dpop"
	deviceauthmodel "github.com/ManuGH/xg2g/internal/domain/deviceauth/model"
	deviceauthstore "github.com/ManuGH/xg2g/internal/domain/deviceauth/store"
	"github.com/ManuGH/xg2g/internal/domain/identity"
	identitystore "github.com/ManuGH/xg2g/internal/domain/identity/store"
	sessionmodel "github.com/ManuGH/xg2g/internal/domain/session/model"
	sessionstore "github.com/ManuGH/xg2g/internal/domain/session/store"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/ManuGH/xg2g/internal/problemcode"
)

// End-to-end proof of the converged device auth contract, over the production
// v3 router and real SQLite on both sides.
//
// Determinism is deliberate: every test gets its own t.TempDir() stores, its
// own freshly generated keys and its own pairing, and nothing is shared between
// test functions. A flaky auth test is worse than no auth test, because a red
// run stops meaning anything.

const e2eHost = "https://xg2g.test"

type deviceAuthE2E struct {
	t            *testing.T
	srv          *Server
	handler      http.Handler
	identity     *identity.Service
	deviceAuthDB *deviceauthstore.SqliteStore
	sessions     *sessionstore.MemoryStore
}

func newDeviceAuthE2E(t *testing.T) *deviceAuthE2E {
	t.Helper()

	dir := t.TempDir()

	deviceAuthStore, err := deviceauthstore.NewSqliteStore(filepath.Join(dir, "deviceauth.sqlite"))
	require.NoError(t, err, "open deviceauth store")
	t.Cleanup(func() { _ = deviceAuthStore.Close() })

	identityStore, err := identitystore.OpenSQLite(filepath.Join(dir, "identity.sqlite"), sqlite.DefaultConfig())
	require.NoError(t, err, "open identity store")
	t.Cleanup(func() { _ = identityStore.Close() })

	srv := NewServer(config.AppConfig{}, nil, nil)
	sessions := sessionstore.NewMemoryStore()
	srv.SetDependencies(Dependencies{DeviceAuthStore: deviceAuthStore, Store: sessions})

	svc := identity.NewService(identity.Config{}, identityStore)
	require.NoError(t, identityStore.PutUser(t.Context(), &identity.User{
		ID:       "usr_viewer",
		Username: "viewer",
		Role:     identity.RoleAdmin,
	}))
	srv.SetIdentityService(svc)

	// Only the admin side is stubbed. A request presenting a device credential
	// runs the real middleware, so anything asserting on *which device* is
	// calling is asserting on the production path rather than on a principal
	// this test fabricated.
	//
	// The two cannot simply be two handlers: the router takes `authMiddleware`
	// as a method value and re-reads the override on every request, so swapping
	// the field after building would change both.
	adminStandIn := testPrincipalAuthMiddleware(t, []string{"v3:admin"})
	srv.AuthMiddlewareOverride = func(next http.Handler) http.Handler {
		stubbed, real := adminStandIn(next), srv.authMiddlewareImpl(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/approve") {
				stubbed.ServeHTTP(w, r)
				return
			}
			real.ServeHTTP(w, r)
		})
	}

	handler, err := newHandlerWithMiddlewares(srv, config.AppConfig{}, nil)
	require.NoError(t, err, "build production v3 handler")

	return &deviceAuthE2E{
		t:            t,
		srv:          srv,
		handler:      handler,
		identity:     svc,
		deviceAuthDB: deviceAuthStore,
		sessions:     sessions,
	}
}

// MARK: - Keys and proofs

type deviceKey struct {
	private *ecdsa.PrivateKey
}

func newDeviceKey(t *testing.T) deviceKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return deviceKey{private: key}
}

// jwk uses FillBytes rather than Bytes: a coordinate short of 32 bytes hashes
// to a different thumbprint, and enrollment rejects it for exactly that reason.
func (k deviceKey) jwk() identity.JWKECPublicKey {
	return identity.JWKECPublicKey{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(k.private.PublicKey.X.FillBytes(make([]byte, 32))),
		Y:   base64.RawURLEncoding.EncodeToString(k.private.PublicKey.Y.FillBytes(make([]byte, 32))),
	}
}

func (k deviceKey) thumbprint(t *testing.T) string {
	t.Helper()
	jkt, err := identity.ComputeJWKThumbprint(k.jwk())
	require.NoError(t, err)
	return jkt
}

type proofOverrides struct {
	htm         string
	htu         string
	accessToken string
	omitATH     bool
}

// proof builds a DPoP proof as a compliant client would: ES256 over the signing
// input, signature as raw R||S per RFC 7518 §3.4 — never DER.
func (k deviceKey) proof(t *testing.T, method, url string, o proofOverrides) string {
	t.Helper()

	jwkBytes, err := json.Marshal(k.jwk())
	require.NoError(t, err)
	var proofJWK dpop.JWKECPublicKey
	require.NoError(t, json.Unmarshal(jwkBytes, &proofJWK))

	htm, htu := method, url
	if o.htm != "" {
		htm = o.htm
	}
	if o.htu != "" {
		htu = o.htu
	}

	payload := dpop.ProofPayload{
		JTI: fmt.Sprintf("jti-%d-%d", time.Now().UnixNano(), len(url)),
		HTM: htm,
		HTU: htu,
		IAT: time.Now().UTC().Unix(),
	}
	if o.accessToken != "" && !o.omitATH {
		payload.ATH = dpop.ComputeAccessTokenHash(o.accessToken)
	}

	headerJSON, err := json.Marshal(dpop.ProofHeader{Typ: "dpop+jwt", Alg: "ES256", JWK: proofJWK})
	require.NoError(t, err)
	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON)
	digest := sha256.Sum256([]byte(signingInput))

	r, s, err := ecdsa.Sign(rand.Reader, k.private, digest[:])
	require.NoError(t, err)
	raw := make([]byte, 64)
	r.FillBytes(raw[:32])
	s.FillBytes(raw[32:])

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(raw)
}

// MARK: - Requests

func (e *deviceAuthE2E) do(method, path string, body any, headers map[string]string) *http.Response {
	e.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(e.t, err)
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, e2eHost+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Result()
}

func (e *deviceAuthE2E) newRequest(method, path string, body any) *http.Request {
	e.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(e.t, err)
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, e2eHost+path, reader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	return req
}

// asDevice issues a request the way an enrolled client does: the DPoP-bound
// access token in `Authorization`, and a fresh proof over this exact target
// with `ath` binding it to that token. Runs against the real auth middleware.
func (e *deviceAuthE2E) asDevice(key deviceKey, method, path, accessToken string, body any) *http.Response {
	e.t.Helper()

	req := e.newRequest(method, path, body)
	req.Header.Set("Authorization", "DPoP "+accessToken)
	req.Header.Set("DPoP", key.proof(e.t, method, e2eHost+path, proofOverrides{accessToken: accessToken}))

	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec.Result()
}

func decodeInto(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

// pairAndExchange runs the full enrollment for a key and returns the response.
func (e *deviceAuthE2E) pairAndExchange(key deviceKey) (ExchangePairingResponse, int) {
	e.t.Helper()

	startResp := e.do(http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceName":             "E2E Device",
		"deviceType":             "android_tv",
		"requestedPolicyProfile": "tv-default",
	}, nil)
	require.Equal(e.t, http.StatusCreated, startResp.StatusCode)
	var started StartPairingResponse
	decodeInto(e.t, startResp, &started)

	approveResp := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/approve", map[string]any{
		"approvedPolicyProfile": "tv-default",
	}, nil)
	require.Equal(e.t, http.StatusOK, approveResp.StatusCode)

	exchangeResp := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
		"deviceJwk":     key.jwk(),
	}, nil)

	status := exchangeResp.StatusCode
	var exchanged ExchangePairingResponse
	if status == http.StatusOK {
		decodeInto(e.t, exchangeResp, &exchanged)
	} else {
		_ = exchangeResp.Body.Close()
	}
	return exchanged, status
}

// MARK: - The happy path

func TestDeviceAuthE2E_EnrollmentBindsGrantToTheDeviceKey(t *testing.T) {
	e := newDeviceAuthE2E(t)
	keyA := newDeviceKey(t)

	enrolled, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	require.NotEmpty(t, enrolled.DeviceId)
	require.NotEmpty(t, enrolled.AccessToken)
	require.NotEmpty(t, enrolled.RefreshToken)
	require.Equal(t, "DPoP", enrolled.TokenType, "the access token must be DPoP-bound, not bearer")

	// The device identity exists in identity, keyed by the server-computed
	// thumbprint — the client never supplies it.
	device, err := e.identity.Store().GetDeviceByThumbprint(t.Context(), keyA.thumbprint(t))
	require.NoError(t, err)
	require.NotNil(t, device, "the enrolled device must exist in identity")
	require.Equal(t, enrolled.DeviceId, device.ID)

	// The ownership boundary: deviceauth keeps only the consumed bootstrap.
	assertDeviceAuthHoldsNoDurableState(t, e.srv, enrolled.DeviceId)
}

func TestDeviceAuthE2E_RefreshRotatesAndRebinds(t *testing.T) {
	e := newDeviceAuthE2E(t)
	keyA := newDeviceKey(t)
	enrolled, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	path := "/api/v3/auth/device/refresh"
	resp := e.do(http.MethodPost, path,
		map[string]any{"refresh_token": enrolled.RefreshToken},
		map[string]string{"DPoP": keyA.proof(t, http.MethodPost, e2eHost+path, proofOverrides{})})
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rotated identity.DeviceGrantResult
	decodeInto(t, resp, &rotated)
	require.NotEmpty(t, rotated.AccessToken)
	require.NotEmpty(t, rotated.RefreshToken)
	require.NotEqual(t, enrolled.RefreshToken, rotated.RefreshToken, "the refresh token must rotate")
	require.Equal(t, "DPoP", rotated.TokenType)
}

// MARK: - Breaking the contract

func TestDeviceAuthE2E_RefreshReplayIsRejected(t *testing.T) {
	e := newDeviceAuthE2E(t)
	keyA := newDeviceKey(t)
	enrolled, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	path := "/api/v3/auth/device/refresh"
	first := e.do(http.MethodPost, path,
		map[string]any{"refresh_token": enrolled.RefreshToken},
		map[string]string{"DPoP": keyA.proof(t, http.MethodPost, e2eHost+path, proofOverrides{})})
	require.Equal(t, http.StatusOK, first.StatusCode)
	_ = first.Body.Close()

	// Presenting the already-rotated token again is a replay.
	replay := e.do(http.MethodPost, path,
		map[string]any{"refresh_token": enrolled.RefreshToken},
		map[string]string{"DPoP": keyA.proof(t, http.MethodPost, e2eHost+path, proofOverrides{})})
	require.Equal(t, http.StatusUnauthorized, replay.StatusCode, "a replayed refresh token must be refused")
	_ = replay.Body.Close()
}

func TestDeviceAuthE2E_ForeignKeyCannotRefresh(t *testing.T) {
	e := newDeviceAuthE2E(t)
	keyA := newDeviceKey(t)
	keyB := newDeviceKey(t)
	enrolled, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	path := "/api/v3/auth/device/refresh"
	resp := e.do(http.MethodPost, path,
		map[string]any{"refresh_token": enrolled.RefreshToken},
		map[string]string{"DPoP": keyB.proof(t, http.MethodPost, e2eHost+path, proofOverrides{})})

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"a proof from a different key must not refresh another device's grant")
	_ = resp.Body.Close()
}

func TestDeviceAuthE2E_MissingProofIsRejected(t *testing.T) {
	e := newDeviceAuthE2E(t)
	keyA := newDeviceKey(t)
	enrolled, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	resp := e.do(http.MethodPost, "/api/v3/auth/device/refresh",
		map[string]any{"refresh_token": enrolled.RefreshToken}, nil)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode, "refresh without a DPoP proof must fail")
	_ = resp.Body.Close()
}

func TestDeviceAuthE2E_TamperedProofClaimsAreRejected(t *testing.T) {
	e := newDeviceAuthE2E(t)
	keyA := newDeviceKey(t)
	enrolled, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	path := "/api/v3/auth/device/refresh"
	for name, override := range map[string]proofOverrides{
		"wrong htu": {htu: e2eHost + "/api/v3/services"},
		"wrong htm": {htm: http.MethodGet},
	} {
		t.Run(name, func(t *testing.T) {
			resp := e.do(http.MethodPost, path,
				map[string]any{"refresh_token": enrolled.RefreshToken},
				map[string]string{"DPoP": keyA.proof(t, http.MethodPost, e2eHost+path, override)})

			require.NotEqual(t, http.StatusOK, resp.StatusCode, "a tampered %s must not be accepted", name)
			_ = resp.Body.Close()
		})
	}
}

func TestDeviceAuthE2E_MalformedDeviceKeyIsRejectedBeforeConsumingThePairing(t *testing.T) {
	e := newDeviceAuthE2E(t)

	startResp := e.do(http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceName": "E2E Device", "deviceType": "android_tv",
	}, nil)
	require.Equal(t, http.StatusCreated, startResp.StatusCode)
	var started StartPairingResponse
	decodeInto(t, startResp, &started)

	approve := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/approve", map[string]any{}, nil)
	require.Equal(t, http.StatusOK, approve.StatusCode)
	_ = approve.Body.Close()

	// A coordinate that is one byte short: same number, different thumbprint.
	key := newDeviceKey(t)
	bad := key.jwk()
	bad.X = base64.RawURLEncoding.EncodeToString(make([]byte, 31))

	badResp := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
		"deviceJwk":     bad,
	}, nil)
	require.Equal(t, http.StatusBadRequest, badResp.StatusCode)
	_ = badResp.Body.Close()

	// The pairing must still be usable: a client-side formatting mistake may
	// not cost the user their enrollment.
	good := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", map[string]any{
		"pairingSecret": started.PairingSecret,
		"deviceJwk":     key.jwk(),
	}, nil)
	require.Equal(t, http.StatusOK, good.StatusCode,
		"an invalid key must be rejected before the pairing is consumed")
	_ = good.Body.Close()
}

func TestDeviceAuthE2E_ConsumedPairingCannotBeExchangedTwice(t *testing.T) {
	e := newDeviceAuthE2E(t)

	startResp := e.do(http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceName": "E2E Device", "deviceType": "android_tv",
	}, nil)
	var started StartPairingResponse
	decodeInto(t, startResp, &started)
	approve := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/approve", map[string]any{}, nil)
	_ = approve.Body.Close()

	key := newDeviceKey(t)
	body := map[string]any{"pairingSecret": started.PairingSecret, "deviceJwk": key.jwk()}

	first := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", body, nil)
	require.Equal(t, http.StatusOK, first.StatusCode)
	_ = first.Body.Close()

	second := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", body, nil)
	require.Equal(t, http.StatusGone, second.StatusCode,
		"a consumed pairing must report a defined terminal state, not mint a second grant")
	_ = second.Body.Close()
}

// Consumption is the single-use synchronisation boundary. If both requests can
// reach enrollment, UpdatePairing is not the boundary we believe it is.
func TestDeviceAuthE2E_ParallelExchangeYieldsExactlyOneWinner(t *testing.T) {
	e := newDeviceAuthE2E(t)

	startResp := e.do(http.MethodPost, "/api/v3/pairing/start", map[string]any{
		"deviceName": "E2E Device", "deviceType": "android_tv",
	}, nil)
	var started StartPairingResponse
	decodeInto(t, startResp, &started)
	approve := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/approve", map[string]any{}, nil)
	_ = approve.Body.Close()

	key := newDeviceKey(t)
	body := map[string]any{"pairingSecret": started.PairingSecret, "deviceJwk": key.jwk()}

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	start := make(chan struct{})
	for i := range statuses {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			resp := e.do(http.MethodPost, "/api/v3/pairing/"+started.PairingId+"/exchange", body, nil)
			statuses[idx] = resp.StatusCode
			_ = resp.Body.Close()
		}(i)
	}
	close(start)
	wg.Wait()

	succeeded := 0
	for _, s := range statuses {
		if s == http.StatusOK {
			succeeded++
		}
	}
	require.Equal(t, 1, succeeded, "exactly one concurrent exchange may succeed, got statuses %v", statuses)
}

// A credential from the retired model must be told to re-pair, not handed an
// anonymous 401 that is indistinguishable from a broken token. The row is
// inserted at the storage layer because nothing writes these any more — which
// is exactly the situation a real upgraded installation is in.
func TestDeviceAuthE2E_LegacyGrantRequiresRePairing(t *testing.T) {
	e := newDeviceAuthE2E(t)

	const legacyToken = "legacy-access-token"
	now := time.Now().UTC()

	_, err := e.deviceAuthDB.DB.Exec(
		`INSERT INTO devices (device_id, owner_id, device_name, device_type, policy_profile,
		 capabilities_json, created_at_ms, last_seen_at_ms, revoked_at_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		"dev-legacy", "viewer", "Old Shield", "android_tv", "default", "{}", now.UnixMilli(),
	)
	require.NoError(t, err, "seed legacy device")

	_, err = e.deviceAuthDB.DB.Exec(
		`INSERT INTO access_sessions (session_id, subject_id, device_id, token_hash, policy_version,
		 scopes_json, auth_strength, issued_at_ms, expires_at_ms, revoked_at_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		"sess-legacy", "viewer", "dev-legacy", deviceauthmodel.HashOpaqueSecret(legacyToken),
		"v1", `["v3:read"]`, "paired_device", now.UnixMilli(), now.Add(time.Hour).UnixMilli(),
	)
	require.NoError(t, err, "seed legacy session")

	// Authenticate through the real stack rather than the test override.
	e.srv.AuthMiddlewareOverride = nil
	handler, err := newHandlerWithMiddlewares(e.srv, config.AppConfig{}, nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, e2eHost+"/api/v3/services/bouquets", nil)
	req.Header.Set("Authorization", "Bearer "+legacyToken)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code,
		"a legacy grant must be refused with a re-pair signal, not a bearer fallback and not a bare 401")

	var problem struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &problem))
	require.Equal(t, problemcode.CodeDeviceReauthRequired, problem.Code)
}

// MARK: - Playback Tickets

// An enrolled device exchanges its DPoP credentials for a session-scoped
// playback ticket, and uses it to play HLS media. The ticket must follow the
// session lifecycle: valid while the session is live, dead the moment it reaches
// a terminal state, and refused on any other session or API endpoint.
func TestDeviceAuthE2E_PlaybackTicketFullLifecycle(t *testing.T) {
	e := newDeviceAuthE2E(t)
	key := newDeviceKey(t)
	enrolled, status := e.pairAndExchange(key)
	require.Equal(t, http.StatusOK, status)

	const sessionID = "11111111-1111-1111-1111-111111111111"
	require.NoError(t, e.sessions.PutSession(t.Context(), &sessionmodel.SessionRecord{
		SessionID: sessionID,
		State:     sessionmodel.SessionReady,
	}))

	// 1. Enrolled device requests a playback ticket via DPoP
	ticketResp := e.asDevice(key, http.MethodPost, "/api/v3/sessions/"+sessionID+"/playback-ticket", enrolled.AccessToken, nil)
	require.Equal(t, http.StatusCreated, ticketResp.StatusCode)

	var ticket playbackTicketResponse
	decodeInto(t, ticketResp, &ticket)
	require.Equal(t, sessionID, ticket.SessionID)
	require.NotEmpty(t, ticket.Ticket)
	require.Equal(t, "xg2g_playback", ticket.Cookie)
	require.Equal(t, "/api/v3/sessions/"+sessionID+"/hls/", ticket.Path)

	mediaPath := "/api/v3/sessions/" + sessionID + "/hls/index.m3u8"

	// 2. A media request carrying the ticket passes authentication (non-401).
	mediaReq := e.newRequest(http.MethodGet, mediaPath, nil)
	mediaReq.AddCookie(&http.Cookie{Name: "xg2g_playback", Value: ticket.Ticket})
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, mediaReq)
	require.NotEqual(t, http.StatusUnauthorized, rec.Code, "valid playback ticket must authenticate media request")

	// 3. Media request without ticket is rejected with 401
	unauthReq := e.newRequest(http.MethodGet, mediaPath, nil)
	recUnauth := httptest.NewRecorder()
	e.handler.ServeHTTP(recUnauth, unauthReq)
	require.Equal(t, http.StatusUnauthorized, recUnauth.Code, "media request without cookie must be rejected")

	// 4. Ticket is refused for another session
	const otherSessionID = "22222222-2222-2222-2222-222222222222"
	require.NoError(t, e.sessions.PutSession(t.Context(), &sessionmodel.SessionRecord{
		SessionID: otherSessionID,
		State:     sessionmodel.SessionReady,
	}))
	otherMediaReq := e.newRequest(http.MethodGet, "/api/v3/sessions/"+otherSessionID+"/hls/index.m3u8", nil)
	otherMediaReq.AddCookie(&http.Cookie{Name: "xg2g_playback", Value: ticket.Ticket})
	recOther := httptest.NewRecorder()
	e.handler.ServeHTTP(recOther, otherMediaReq)
	require.Equal(t, http.StatusUnauthorized, recOther.Code, "ticket for session A must not authenticate session B")

	// 5. Ticket is refused for API routes
	apiReq := e.newRequest(http.MethodGet, "/api/v3/auth/effective-permissions", nil)
	apiReq.AddCookie(&http.Cookie{Name: "xg2g_playback", Value: ticket.Ticket})
	recAPI := httptest.NewRecorder()
	e.handler.ServeHTTP(recAPI, apiReq)
	require.Equal(t, http.StatusUnauthorized, recAPI.Code, "playback ticket must not authenticate API routes")

	// 6. Session transitions to terminal state (SessionStopped) -> Ticket dies immediately
	require.NoError(t, e.sessions.PutSession(t.Context(), &sessionmodel.SessionRecord{
		SessionID: sessionID,
		State:     sessionmodel.SessionStopped,
	}))
	stoppedReq := e.newRequest(http.MethodGet, mediaPath, nil)
	stoppedReq.AddCookie(&http.Cookie{Name: "xg2g_playback", Value: ticket.Ticket})
	recStopped := httptest.NewRecorder()
	e.handler.ServeHTTP(recStopped, stoppedReq)
	require.Equal(t, http.StatusUnauthorized, recStopped.Code, "ticket must fail when session is stopped")

	// 7. Session transitions to SessionFailed -> Ticket dies
	require.NoError(t, e.sessions.PutSession(t.Context(), &sessionmodel.SessionRecord{
		SessionID: sessionID,
		State:     sessionmodel.SessionFailed,
	}))
	failedReq := e.newRequest(http.MethodGet, mediaPath, nil)
	failedReq.AddCookie(&http.Cookie{Name: "xg2g_playback", Value: ticket.Ticket})
	recFailed := httptest.NewRecorder()
	e.handler.ServeHTTP(recFailed, failedReq)
	require.Equal(t, http.StatusUnauthorized, recFailed.Code, "ticket must fail when session is failed")
}

func TestDeviceAuthE2E_PlaybackTicketRequiresAuthentication(t *testing.T) {
	e := newDeviceAuthE2E(t)
	const sessionID = "11111111-1111-1111-1111-111111111111"

	// Unauthenticated request to issue ticket fails
	unauthResp := e.do(http.MethodPost, "/api/v3/sessions/"+sessionID+"/playback-ticket", nil, nil)
	defer func() { _ = unauthResp.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, unauthResp.StatusCode, "issuing a playback ticket requires auth")

	// Foreign DPoP proof fails
	keyA := newDeviceKey(t)
	keyB := newDeviceKey(t)
	enrolledA, status := e.pairAndExchange(keyA)
	require.Equal(t, http.StatusOK, status)

	path := "/api/v3/sessions/" + sessionID + "/playback-ticket"
	badProofResp := e.do(http.MethodPost, path, nil, map[string]string{
		"Authorization": "DPoP " + enrolledA.AccessToken,
		"DPoP":          keyB.proof(t, http.MethodPost, e2eHost+path, proofOverrides{accessToken: enrolledA.AccessToken}),
	})
	defer func() { _ = badProofResp.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, badProofResp.StatusCode, "foreign key proof must be refused")
}
