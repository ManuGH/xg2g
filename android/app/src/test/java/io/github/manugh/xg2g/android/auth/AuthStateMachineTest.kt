package io.github.manugh.xg2g.android.auth

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class AuthStateMachineTest {

    @Test
    fun `initial enrollment transitions correctly based on form factor`() {
        val phoneMachine = AuthStateMachine(isTvDevice = false)
        assertEquals(AuthStateKind.SignedOut, phoneMachine.currentState.kind)

        val phoneEnroll = phoneMachine.startEnrollment(setupToken = "xg2g_setup_123")
        assertEquals(AuthStateKind.PasskeyRequired, phoneEnroll.kind)
        assertEquals("xg2g_setup_123", (phoneEnroll as AuthState.PasskeyRequired).setupToken)

        val tvMachine = AuthStateMachine(isTvDevice = true)
        val tvEnroll = tvMachine.startEnrollment(pairingCode = "ABCD-EFGH", qrUrl = "https://xg2g/pair")
        assertEquals(AuthStateKind.PairingRequired, tvEnroll.kind)
        assertEquals("ABCD-EFGH", (tvEnroll as AuthState.PairingRequired).pairingCode)
    }

    @Test
    fun `activation transitions to DeviceGrantActive`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.startEnrollment()

        val active = machine.activateDeviceGrant(
            deviceGrantId = "dgr_100",
            accessToken = "at_dpop_secret",
            jktThumbprint = "jkt_hash_xyz",
            expiresAtEpochMs = 120_000L
        )

        assertEquals(AuthStateKind.DeviceGrantActive, active.kind)
        val activeState = active as AuthState.DeviceGrantActive
        assertEquals("dgr_100", activeState.deviceGrantId)
        assertEquals("at_dpop_secret", activeState.accessToken)
        assertEquals("jkt_hash_xyz", activeState.jktThumbprint)
    }

    @Test
    fun `network error during Refreshing remains in Refreshing with backoff and does NOT log out`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_old", "jkt_100", 50_000L)

        // 1. Token expires -> transition to Refreshing
        val refreshing = machine.triggerTokenRefresh()
        assertEquals(AuthStateKind.Refreshing, refreshing.kind)
        assertEquals(1, (refreshing as AuthState.Refreshing).attemptCount)

        // 2. Network error 1 (Timeout / Offline) -> Stay in Refreshing with attempt 2
        val netErr1 = machine.handleRefreshError("SocketTimeoutException: network unavailable", isNetworkError = true, isRevoked = false)
        assertEquals(AuthStateKind.Refreshing, netErr1.kind)
        assertEquals(2, (netErr1 as AuthState.Refreshing).attemptCount)
        assertEquals("SocketTimeoutException: network unavailable", netErr1.lastError)

        // 3. Network error 2 -> Stay in Refreshing with attempt 3
        val netErr2 = machine.handleRefreshError("ConnectException: failed to connect", isNetworkError = true, isRevoked = false)
        assertEquals(AuthStateKind.Refreshing, netErr2.kind)
        assertEquals(3, (netErr2 as AuthState.Refreshing).attemptCount)

        // User identity is NOT cleared, session is NOT forced to ReauthRequired or SignedOut
        assertTrue("Previous grant details MUST be preserved during network outages", netErr2.previousGrant != null)
        assertEquals("dgr_100", netErr2.previousGrant?.deviceGrantId)
    }

    @Test
    fun `auth error during Refreshing transitions to ReauthRequired`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_old", "jkt_100", 50_000L)
        machine.triggerTokenRefresh()

        val authErr = machine.handleRefreshError("HTTP 401: invalid_grant", isNetworkError = false, isRevoked = false)
        assertEquals(AuthStateKind.ReauthRequired, authErr.kind)
        assertEquals("HTTP 401: invalid_grant", (authErr as AuthState.ReauthRequired).reason)
    }

    @Test
    fun `admin revocation strictly transitions to Revoked`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_old", "jkt_100", 50_000L)

        val revoked = machine.revoke("Admin revoked device access from WebUI")
        assertEquals(AuthStateKind.Revoked, revoked.kind)
        assertEquals("Admin revoked device access from WebUI", (revoked as AuthState.Revoked).reason)
    }

    @Test
    fun `Revoked state forbids automatic token refresh`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_old", "jkt_100", 50_000L)
        machine.revoke("Device revoked")

        try {
            machine.triggerTokenRefresh()
            fail("Expected IllegalStateException when refreshing from Revoked state")
        } catch (e: IllegalStateException) {
            assertTrue(e.message!!.contains("Cannot refresh token from Revoked state"))
        }

        try {
            machine.activateDeviceGrant("dgr_new", "at_new", "jkt_100", 120_000L)
            fail("Expected IllegalStateException when activating grant directly from Revoked state")
        } catch (e: IllegalStateException) {
            assertTrue(e.message!!.contains("Cannot activate device grant directly from Revoked state"))
        }
    }

    @Test
    fun `Revoked state requires explicit user re-enrollment to exit`() {
        val machine = AuthStateMachine(isTvDevice = false)
        machine.activateDeviceGrant("dgr_100", "at_old", "jkt_100", 50_000L)
        machine.revoke("Device revoked")

        // Explicit re-enrollment action
        val resetState = machine.resetToEnrollment(setupToken = "xg2g_fresh_token")
        assertEquals(AuthStateKind.PasskeyRequired, resetState.kind)
        assertEquals("xg2g_fresh_token", (resetState as AuthState.PasskeyRequired).setupToken)
    }

    @Test
    fun `SoftwareDPoPProvider generates valid ES256 proof and JWK thumbprint`() {
        val dpop = SoftwareDPoPProvider()
        val jkt = dpop.getJWKThumbprint()
        assertTrue("JWK thumbprint must not be empty", jkt.isNotEmpty())

        val proof = dpop.createProof("POST", "https://xg2g.local/api/v3/auth/device/grant", accessToken = "at_dpop_123")
        val parts = proof.split(".")
        assertEquals(3, parts.size)
        assertTrue("Proof header must start with valid B64", parts[0].isNotEmpty())
    }
}
