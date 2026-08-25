package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.transport.DeviceAuthTransport
import io.github.manugh.xg2g.android.transport.RefreshedDeviceSession
import io.github.manugh.xg2g.android.transport.auth.NativeDeviceAuthRepository
import java.io.IOException
import kotlinx.coroutines.runBlocking
import okhttp3.Headers
import okhttp3.HttpUrl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class NativeDeviceAuthRepositoryTest {

    private class FakeStore(var initial: PersistedDeviceAuthState? = null) : PersistedDeviceAuthStateStore {
        var saved: PersistedDeviceAuthState? = initial

        override fun load(): PersistedDeviceAuthState? = saved

        override fun save(state: PersistedDeviceAuthState) {
            saved = state
        }

        override fun clear() {
            saved = null
        }
    }

    private class FakeTransport : DeviceAuthTransport {
        var shouldThrowNetworkError = false
        var shouldThrowInvalidGrant = false
        var shouldThrowRevoked = false
        var refreshedSession: RefreshedDeviceSession? = null

        override suspend fun refreshSession(
            uiBaseUrl: HttpUrl,
            deviceGrantId: String,
            deviceGrant: String
        ): RefreshedDeviceSession {
            if (shouldThrowNetworkError) {
                throw IOException("SocketTimeoutException: network offline")
            }
            if (shouldThrowInvalidGrant) {
                throw RuntimeException("HTTP 401: invalid_grant")
            }
            if (shouldThrowRevoked) {
                throw RuntimeException("HTTP 403: device revoked by admin")
            }
            return refreshedSession ?: RefreshedDeviceSession(
                rotatedDeviceGrantId = "dgr_rotated",
                rotatedDeviceGrant = "secret_rotated",
                accessSessionId = "sess_100",
                accessToken = "at_dpop_fresh",
                accessTokenExpiresAtEpochMs = 120_000L,
                policyVersion = "v1",
                endpoints = emptyList()
            )
        }

        override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {}
    }

    @Test
    fun `hydratePersistedStateOnLaunch with valid stored token enters DeviceGrantActive directly`() = runBlocking {
        val store = FakeStore(
            PersistedDeviceAuthState(
                serverUrl = "https://xg2g.local/ui/",
                deviceGrantId = "dgr_100",
                deviceGrant = "secret_100",
                accessToken = "at_valid",
                accessTokenExpiresAtEpochMs = 100_000L
            )
        )
        val dpop = SoftwareDPoPProvider()
        val machine = AuthStateMachine(isTvDevice = false)
        val transport = FakeTransport()
        val repo = NativeDeviceAuthRepository(store, dpop, machine, transport, nowEpochMs = { 10_000L })

        val state = repo.hydratePersistedStateOnLaunch("https://xg2g.local/ui/")
        assertEquals(AuthStateKind.DeviceGrantActive, state.kind)
        assertEquals("dgr_100", (state as AuthState.DeviceGrantActive).deviceGrantId)
        assertEquals("at_valid", state.accessToken)
    }

    @Test
    fun `hydratePersistedStateOnLaunch with expired token and network error stays in Refreshing with backoff and NO logout`() = runBlocking {
        val store = FakeStore(
            PersistedDeviceAuthState(
                serverUrl = "https://xg2g.local/ui/",
                deviceGrantId = "dgr_100",
                deviceGrant = "secret_100",
                accessToken = "at_stale",
                accessTokenExpiresAtEpochMs = 5_000L // Expired
            )
        )
        val dpop = SoftwareDPoPProvider()
        val machine = AuthStateMachine(isTvDevice = false)
        val transport = FakeTransport().apply { shouldThrowNetworkError = true }
        val repo = NativeDeviceAuthRepository(store, dpop, machine, transport, nowEpochMs = { 10_000L })

        val state = repo.hydratePersistedStateOnLaunch("https://xg2g.local/ui/")
        assertEquals("Expected Refreshing state, got state=$state", AuthStateKind.Refreshing, state.kind)
        val refreshingState = state as AuthState.Refreshing
        assertEquals(2, refreshingState.attemptCount)
        assertTrue("Network error during launch refresh MUST keep credentials and stay in Refreshing", refreshingState.previousGrant != null)
    }

    @Test
    fun `hydratePersistedStateOnLaunch with invalid grant transitions to ReauthRequired`() = runBlocking {
        val store = FakeStore(
            PersistedDeviceAuthState(
                serverUrl = "https://xg2g.local/ui/",
                deviceGrantId = "dgr_stale",
                deviceGrant = "secret_stale",
                accessToken = "at_stale",
                accessTokenExpiresAtEpochMs = 5_000L
            )
        )
        val dpop = SoftwareDPoPProvider()
        val machine = AuthStateMachine(isTvDevice = false)
        val transport = FakeTransport().apply { shouldThrowInvalidGrant = true }
        val repo = NativeDeviceAuthRepository(store, dpop, machine, transport, nowEpochMs = { 10_000L })

        val state = repo.hydratePersistedStateOnLaunch("https://xg2g.local/ui/")
        assertEquals(AuthStateKind.ReauthRequired, state.kind)
        assertTrue((state as AuthState.ReauthRequired).reason.contains("invalid_grant"))
    }

    @Test
    fun `hydratePersistedStateOnLaunch with admin revocation strictly transitions to Revoked`() = runBlocking {
        val store = FakeStore(
            PersistedDeviceAuthState(
                serverUrl = "https://xg2g.local/ui/",
                deviceGrantId = "dgr_revoked",
                deviceGrant = "secret_revoked",
                accessToken = "at_stale",
                accessTokenExpiresAtEpochMs = 5_000L
            )
        )
        val dpop = SoftwareDPoPProvider()
        val machine = AuthStateMachine(isTvDevice = false)
        val transport = FakeTransport().apply { shouldThrowRevoked = true }
        val repo = NativeDeviceAuthRepository(store, dpop, machine, transport, nowEpochMs = { 10_000L })

        val state = repo.hydratePersistedStateOnLaunch("https://xg2g.local/ui/")
        assertEquals(AuthStateKind.Revoked, state.kind)
        assertTrue((state as AuthState.Revoked).reason.contains("revoked"))
    }
}
