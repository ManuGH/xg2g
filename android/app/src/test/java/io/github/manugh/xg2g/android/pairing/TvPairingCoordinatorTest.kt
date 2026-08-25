package io.github.manugh.xg2g.android.pairing

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateKind
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.SoftwareDPoPProvider
import io.github.manugh.xg2g.android.transport.pairing.PairingApiClient
import kotlinx.coroutines.runBlocking
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class TvPairingCoordinatorTest {

    private class FakeStore : PersistedDeviceAuthStateStore {
        var saved: PersistedDeviceAuthState? = null
        override fun load(): PersistedDeviceAuthState? = saved
        override fun save(state: PersistedDeviceAuthState) { saved = state }
        override fun clear() { saved = null }
    }

    @Test
    fun `executePairingFlow displays 8-char code and transitions from PairingRequired to DeviceGrantActive on approval`() = runBlocking {
        val interceptor = Interceptor { chain ->
            val path = chain.request().url.encodedPath
            val body = when {
                path.endsWith("/pairing/start") -> """
                    {
                        "pairingId": "pair_999",
                        "pairingSecret": "sec_999",
                        "userCode": "ABCD-EFGH",
                        "qrPayload": "https://xg2g.local/pair?code=ABCD-EFGH",
                        "expiresAt": "2026-08-13T14:00:00Z"
                    }
                """.trimIndent()

                path.contains("/status") -> """
                    {
                        "pairingId": "pair_999",
                        "status": "approved",
                        "userCode": "ABCD-EFGH",
                        "approvedAt": "2026-08-13T13:30:00Z",
                        "expiresAt": "2026-08-13T14:00:00Z"
                    }
                """.trimIndent()

                path.contains("/exchange") -> """
                    {
                        "pairingId": "pair_999",
                        "deviceId": "dev_tv_100",
                        "deviceGrantId": "dgr_tv_100",
                        "deviceGrant": "grant_tv_secret",
                        "accessSessionId": "sess_tv_100",
                        "accessToken": "at_tv_dpop",
                        "accessTokenExpiresAt": "2026-08-13T15:00:00Z",
                        "policyVersion": "v1",
                        "endpoints": []
                    }
                """.trimIndent()

                else -> "{}"
            }

            Response.Builder()
                .request(chain.request())
                .protocol(Protocol.HTTP_1_1)
                .code(200)
                .message("OK")
                .body(body.toResponseBody("application/json".toMediaType()))
                .build()
        }

        val client = OkHttpClient.Builder().addInterceptor(interceptor).build()
        val baseUrl = "https://xg2g.local"
        val apiClient = PairingApiClient(baseUrl, client)
        val store = FakeStore()
        val dpop = SoftwareDPoPProvider()
        val machine = AuthStateMachine(isTvDevice = true)
        val coordinator = TvPairingCoordinator(apiClient, store, dpop, machine)

        val resultState = coordinator.executePairingFlow(
            baseUrl = baseUrl,
            deviceName = "Wohnzimmer TV",
            pollIntervalMs = 5L,
            maxPollAttempts = 5
        )

        assertEquals(AuthStateKind.DeviceGrantActive, resultState.kind)
        val active = resultState as AuthState.DeviceGrantActive
        assertEquals("dgr_tv_100", active.deviceGrantId)
        assertEquals("at_tv_dpop", active.accessToken)

        // Verify state store
        assertNotNull(store.saved)
        assertEquals("dgr_tv_100", store.saved?.deviceGrantId)
        assertEquals("grant_tv_secret", store.saved?.deviceGrant)
    }

    @Test
    fun `executePairingFlow transitions to ReauthRequired when pairing session expires`() = runBlocking {
        val interceptor = Interceptor { chain ->
            val path = chain.request().url.encodedPath
            val body = when {
                path.endsWith("/pairing/start") -> """
                    {
                        "pairingId": "pair_999",
                        "pairingSecret": "sec_999",
                        "userCode": "ABCD-EFGH",
                        "qrPayload": "https://xg2g.local/pair?code=ABCD-EFGH",
                        "expiresAt": "2026-08-13T14:00:00Z"
                    }
                """.trimIndent()

                path.contains("/status") -> """
                    {
                        "pairingId": "pair_999",
                        "status": "expired",
                        "userCode": "ABCD-EFGH",
                        "expiresAt": "2026-08-13T14:00:00Z"
                    }
                """.trimIndent()

                else -> "{}"
            }

            Response.Builder()
                .request(chain.request())
                .protocol(Protocol.HTTP_1_1)
                .code(200)
                .message("OK")
                .body(body.toResponseBody("application/json".toMediaType()))
                .build()
        }

        val client = OkHttpClient.Builder().addInterceptor(interceptor).build()
        val baseUrl = "https://xg2g.local"
        val apiClient = PairingApiClient(baseUrl, client)
        val store = FakeStore()
        val dpop = SoftwareDPoPProvider()
        val machine = AuthStateMachine(isTvDevice = true)
        val coordinator = TvPairingCoordinator(apiClient, store, dpop, machine)

        val resultState = coordinator.executePairingFlow(
            baseUrl = baseUrl,
            deviceName = "Wohnzimmer TV",
            pollIntervalMs = 5L,
            maxPollAttempts = 5
        )

        assertEquals(AuthStateKind.ReauthRequired, resultState.kind)
    }
}
