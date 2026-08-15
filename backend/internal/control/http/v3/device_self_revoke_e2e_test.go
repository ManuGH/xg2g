// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Self-revocation, over the production router and real SQLite.
//
// The client contract is remote-first: an app destroys its device key only
// after this endpoint confirms. That makes these assertions load-bearing in an
// unusual way — a false success here does not merely leave a stale credential,
// it tells a device to throw away the only key that could ever have revoked it.

const revokePath = "/api/v3/auth/device/revoke"

// probePath is an authenticated route used to ask one question: does this
// credential still work?
//
// A live enrolled device gets 200 here — measured, not assumed — so the
// before/after guards assert 200 and 401 exactly rather than "not 401". The
// weaker form would also pass if the setup had silently degraded to a 403 or a
// 404, which would make the later 401 prove nothing at all.
const probePath = "/api/v3/auth/effective-permissions"

// MARK: - 1. A device revokes itself

func TestDeviceSelfRevoke_OwnKeySucceeds(t *testing.T) {
	e := newDeviceAuthE2E(t)
	key := newDeviceKey(t)

	enrolled, status := e.pairAndExchange(key)
	require.Equal(t, http.StatusOK, status)

	// The credential works before revocation, so a 401 afterwards means the
	// revocation did it — not that the setup never authenticated at all.
	before := e.asDevice(key, http.MethodGet, probePath, enrolled.AccessToken, nil)
	defer func() { _ = before.Body.Close() }()
	require.Equal(t, http.StatusOK, before.StatusCode,
		"the freshly enrolled device must authenticate, or this test proves nothing")

	resp := e.asDevice(key, http.MethodPost, revokePath, enrolled.AccessToken, nil)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// MARK: - 2. After revocation, neither half of the credential set works

func TestDeviceSelfRevoke_KillsAccessAndRefresh(t *testing.T) {
	e := newDeviceAuthE2E(t)
	key := newDeviceKey(t)

	enrolled, status := e.pairAndExchange(key)
	require.Equal(t, http.StatusOK, status)

	revoked := e.asDevice(key, http.MethodPost, revokePath, enrolled.AccessToken, nil)
	_ = revoked.Body.Close()
	require.Equal(t, http.StatusNoContent, revoked.StatusCode)

	// The access token is dead immediately — not merely unrenewable.
	after := e.asDevice(key, http.MethodGet, probePath, enrolled.AccessToken, nil)
	defer func() { _ = after.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, after.StatusCode,
		"a revoked access token must stop authenticating at once")

	// And the refresh token cannot mint a replacement. Revoking only the access
	// token would buy the device a few minutes of inconvenience and nothing
	// more.
	refresh := e.do(http.MethodPost, "/api/v3/auth/device/refresh",
		map[string]any{"refresh_token": enrolled.RefreshToken},
		map[string]string{"DPoP": key.proof(t, http.MethodPost, e2eHost+"/api/v3/auth/device/refresh", proofOverrides{})})
	defer func() { _ = refresh.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, refresh.StatusCode,
		"a revoked refresh family must not issue a new grant")
}

// MARK: - 3. A device can revoke only itself

// The endpoint accepts no device identifier, so the only way to ask for someone
// else's device is to send one anyway. It must be ignored: the caller's own
// credentials go, and the named device is untouched.
func TestDeviceSelfRevoke_IgnoresADeviceIDInTheBody(t *testing.T) {
	e := newDeviceAuthE2E(t)
	victim, attacker := newDeviceKey(t), newDeviceKey(t)

	victimEnrolled, status := e.pairAndExchange(victim)
	require.Equal(t, http.StatusOK, status)
	attackerEnrolled, status := e.pairAndExchange(attacker)
	require.Equal(t, http.StatusOK, status)
	require.NotEqual(t, victimEnrolled.DeviceID, attackerEnrolled.DeviceID)

	resp := e.asDevice(attacker, http.MethodPost, revokePath, attackerEnrolled.AccessToken,
		map[string]any{"deviceId": victimEnrolled.DeviceID})
	_ = resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// The named device is untouched.
	victimProbe := e.asDevice(victim, http.MethodGet, probePath, victimEnrolled.AccessToken, nil)
	defer func() { _ = victimProbe.Body.Close() }()
	require.Equal(t, http.StatusOK, victimProbe.StatusCode,
		"a device id in the body must not select whose credentials are revoked")

	// The caller revoked itself, which is the only thing it was ever able to do.
	attackerProbe := e.asDevice(attacker, http.MethodGet, probePath, attackerEnrolled.AccessToken, nil)
	defer func() { _ = attackerProbe.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, attackerProbe.StatusCode)
}

// A proof signed by a key that is not the one the access token is bound to must
// not authenticate at all — so it cannot revoke anything, its own device
// included.
func TestDeviceSelfRevoke_ForeignKeyCannotRevoke(t *testing.T) {
	e := newDeviceAuthE2E(t)
	owner, foreign := newDeviceKey(t), newDeviceKey(t)

	enrolled, status := e.pairAndExchange(owner)
	require.Equal(t, http.StatusOK, status)

	resp := e.asDevice(foreign, http.MethodPost, revokePath, enrolled.AccessToken, nil)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"the proof key must match the token's binding")

	// The owner is unaffected: a rejected attempt must not have side effects.
	probe := e.asDevice(owner, http.MethodGet, probePath, enrolled.AccessToken, nil)
	defer func() { _ = probe.Body.Close() }()
	require.Equal(t, http.StatusOK, probe.StatusCode,
		"a failed revocation attempt must leave the real device working")
}

// Without a DPoP proof there is no device context, so there is nothing this
// endpoint could act on.
func TestDeviceSelfRevoke_RequiresADeviceCredential(t *testing.T) {
	e := newDeviceAuthE2E(t)
	key := newDeviceKey(t)

	enrolled, status := e.pairAndExchange(key)
	require.Equal(t, http.StatusOK, status)

	req := e.newRequest(http.MethodPost, revokePath, nil)
	req.Header.Set("Authorization", "DPoP "+enrolled.AccessToken)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode,
		"a sender-constrained token without its proof is not a device credential")
}

// MARK: - Idempotence

// The client destroys its local key only after a confirmed revocation, so a
// lost response must be retryable. If the retry failed, the device would be
// stuck holding material it can never clear.
func TestDeviceSelfRevoke_IsIdempotentWithinTheTokenLifetime(t *testing.T) {
	e := newDeviceAuthE2E(t)
	key := newDeviceKey(t)

	enrolled, status := e.pairAndExchange(key)
	require.Equal(t, http.StatusOK, status)

	first := e.asDevice(key, http.MethodPost, revokePath, enrolled.AccessToken, nil)
	_ = first.Body.Close()
	require.Equal(t, http.StatusNoContent, first.StatusCode)

	// The second attempt can no longer authenticate — the credential it would
	// use is exactly the one the first call retired. That is a defined,
	// non-ambiguous answer for the client: 401 here means "already gone", which
	// it treats the same as success.
	second := e.asDevice(key, http.MethodPost, revokePath, enrolled.AccessToken, nil)
	defer func() { _ = second.Body.Close() }()
	require.Equal(t, http.StatusUnauthorized, second.StatusCode)
}
