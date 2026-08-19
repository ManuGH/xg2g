package io.github.manugh.xg2g.android.pairing

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateKind
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.SoftwareDPoPProvider
import io.github.manugh.xg2g.android.contract.Xg2gContractException
import kotlinx.coroutines.runBlocking
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The fixtures here are the shapes ExchangePairing and GetPairingStatus actually
 * write in backend/internal/control/http/v3/handlers_pairing.go, validated
 * against api/openapi.yaml by TestV3Contract_PairingFlowResponsesMatchOpenAPI.
 *
 * They used to be the shapes somebody remembered: deviceGrantId, deviceGrant and
 * accessSessionId, retired on the server long before. The suite stayed green
 * while pairing was broken on every real device, because a hand-written fixture
 * only ever tests the client against the author's belief about the server.
 */
class TvPairingCoordinatorTest {

    private class FakeStore : PersistedDeviceAuthStateStore {
        var saved: PersistedDeviceAuthState? = null
        override fun load(): PersistedDeviceAuthState? = saved
        override fun save(state: PersistedDeviceAuthState) { saved = state }
        override fun clear() { saved = null }
    }

    private companion object {
        const val START_BODY = """
            {
                "pairingId": "pair_999",
                "pairingSecret": "sec_999",
                "userCode": "ABCD-EFGH",
                "qrPayload": "https://xg2g.local/pair?code=ABCD-EFGH",
                "expiresAt": "2026-08-13T14:00:00Z"
            }
        """

        const val EXCHANGE_BODY = """
            {
                "pairingId": "pair_999",
                "deviceId": "dev_tv_100",
                "tokenType": "DPoP",
                "accessToken": "at_tv_dpop",
                "expiresIn": 900,
                "refreshToken": "rt_tv_rotating",
                "scope": "v3:read v3:stream",
                "policyVersion": "v1",
                "endpoints": []
            }
        """

        fun statusBody(status: String, approvedAt: String? = null): String {
            val approved = approvedAt?.let { ""","approvedAt": "$it"""" }.orEmpty()
            return """
                {
                    "pairingId": "pair_999",
                    "status": "$status",
                    "userCode": "ABCD-EFGH",
                    "deviceName": "Wohnzimmer TV",
                    "deviceType": "android_tv",
                    "expiresAt": "2026-08-13T14:00:00Z"$approved
                }
            """
        }
    }

    private fun serving(
        startBody: String = START_BODY,
        statusBody: String,
        exchangeBody: String = EXCHANGE_BODY,
        onRequest: (path: String, body: String) -> Unit = { _, _ -> }
    ): OkHttpClient {
        val interceptor = Interceptor { chain ->
            val request = chain.request()
            val path = request.url.encodedPath

            val sent = Buffer().also { request.body?.writeTo(it) }.readUtf8()
            onRequest(path, sent)

            val body = when {
                path.endsWith("/pairing/start") -> startBody
                path.contains("/status") -> statusBody
                path.contains("/exchange") -> exchangeBody
                else -> "{}"
            }

            Response.Builder()
                .request(request)
                .protocol(Protocol.HTTP_1_1)
                .code(200)
                .message("OK")
                .body(body.trimIndent().toResponseBody("application/json".toMediaType()))
                .build()
        }
        return OkHttpClient.Builder().addInterceptor(interceptor).build()
    }

    private fun coordinator(
        client: OkHttpClient,
        store: PersistedDeviceAuthStateStore,
        nowEpochMs: Long = 1_760_000_000_000L
    ) = TvPairingCoordinator(
        pairingApiClient = PairingApiClient("https://xg2g.local", client),
        stateStore = store,
        dpopProvider = SoftwareDPoPProvider(),
        stateMachine = AuthStateMachine(isTvDevice = true),
        nowEpochMs = { nowEpochMs }
    )

    private fun runFlow(coordinator: TvPairingCoordinator): AuthState = runBlocking {
        coordinator.executePairingFlow(
            baseUrl = "https://xg2g.local",
            deviceName = "Wohnzimmer TV",
            pollIntervalMs = 5L,
            maxPollAttempts = 5
        )
    }

    @Test
    fun `approval activates the device grant from the identity-shaped exchange`() {
        val store = FakeStore()
        val now = 1_760_000_000_000L

        val result = runFlow(
            coordinator(
                serving(statusBody = statusBody("approved", approvedAt = "2026-08-13T13:30:00Z")),
                store,
                nowEpochMs = now
            )
        )

        assertEquals(AuthStateKind.DeviceGrantActive, result.kind)
        val active = result as AuthState.DeviceGrantActive
        assertEquals("dev_tv_100", active.deviceGrantId)
        assertEquals("at_tv_dpop", active.accessToken)

        // expiresIn is a lifetime, not an instant: 900s past the injected clock.
        assertEquals(now + 900_000L, active.expiresAtEpochMs)

        val saved = assertNotNull(store.saved).let { store.saved!! }
        assertEquals("dev_tv_100", saved.deviceGrantId)
        assertEquals("rt_tv_rotating", saved.deviceGrant)
        assertEquals("at_tv_dpop", saved.accessToken)
        assertEquals(now + 900_000L, saved.accessTokenExpiresAtEpochMs)
        assertNull("no access session exists in the identity model", saved.accessSessionId)
    }

    @Test
    fun `the exchange request carries the device public key`() {
        val store = FakeStore()
        var exchangeBody: String? = null

        runFlow(
            coordinator(
                serving(
                    statusBody = statusBody("approved"),
                    onRequest = { path, body -> if (path.contains("/exchange")) exchangeBody = body }
                ),
                store
            )
        )

        // The exchange is what binds the issued credentials to the device key.
        // Sending it used to be forgotten entirely; PairingSecretRequest no
        // longer compiles without it.
        val sent = JSONObject(assertNotNull(exchangeBody).let { exchangeBody!! })
        assertEquals("sec_999", sent.getString("pairingSecret"))

        val jwk = sent.getJSONObject("deviceJwk")
        assertEquals("EC", jwk.getString("kty"))
        assertEquals("P-256", jwk.getString("crv"))
        assertTrue("x coordinate must be present", jwk.getString("x").isNotBlank())
        assertTrue("y coordinate must be present", jwk.getString("y").isNotBlank())
    }

    @Test
    fun `an expired pairing ends the flow instead of polling to the attempt limit`() {
        val result = runFlow(coordinator(serving(statusBody = statusBody("expired")), FakeStore()))
        assertEquals(AuthStateKind.ReauthRequired, result.kind)
    }

    @Test
    fun `a revoked pairing is reported as revoked, not as a generic reauth`() {
        // "revoked" and "consumed" are contract states the previous hand-written
        // status check did not know about: it looked for "cancelled" and
        // "rejected", which the server never sends, and polled until timeout.
        val result = runFlow(coordinator(serving(statusBody = statusBody("revoked")), FakeStore()))
        assertEquals(AuthStateKind.Revoked, result.kind)
    }

    @Test
    fun `a consumed pairing is terminal`() {
        val result = runFlow(coordinator(serving(statusBody = statusBody("consumed")), FakeStore()))
        assertEquals(AuthStateKind.ReauthRequired, result.kind)
    }

    @Test
    fun `a response missing a required field fails loudly rather than defaulting`() {
        val withoutRefreshToken = EXCHANGE_BODY.replace("\"refreshToken\": \"rt_tv_rotating\",", "")

        val error = assertThrows(Xg2gContractException::class.java) {
            runFlow(
                coordinator(
                    serving(statusBody = statusBody("approved"), exchangeBody = withoutRefreshToken),
                    FakeStore()
                )
            )
        }

        assertTrue(
            "the failure must name the field: $error",
            error.message!!.contains("refreshToken")
        )
    }

    @Test
    fun `an unknown status value is a contract violation, not a silent pending`() {
        val error = assertThrows(Xg2gContractException::class.java) {
            runFlow(coordinator(serving(statusBody = statusBody("cancelled")), FakeStore()))
        }

        assertTrue(
            "the failure must name the unknown value: $error",
            error.message!!.contains("cancelled")
        )
    }
}
