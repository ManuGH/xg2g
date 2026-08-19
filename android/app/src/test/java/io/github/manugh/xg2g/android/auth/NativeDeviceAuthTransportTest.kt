package io.github.manugh.xg2g.android.auth

import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.IOException
import java.security.KeyPair
import java.security.KeyPairGenerator

class NativeDeviceAuthTransportTest {
    private class CapturingDPoPProvider : DPoPProvider {
        var proofMethod: String? = null
        var proofUrl: String? = null
        var proofAccessToken: String? = null
        var proofCalls = 0

        override fun getOrGenerateKeyPair(): KeyPair =
            KeyPairGenerator.getInstance("EC").apply { initialize(256) }.generateKeyPair()

        override fun getJWKThumbprint(): String = "test-jkt"

        override fun createProof(htm: String, htu: String, accessToken: String?): String {
            proofCalls += 1
            proofMethod = htm
            proofUrl = htu
            proofAccessToken = accessToken
            return "test-dpop-proof"
        }
    }

    @Test
    fun `refresh roots API URL at origin, targets refresh endpoint, and signs the transported URL`() {
        val dpop = CapturingDPoPProvider()
        val request = buildDeviceRefreshRequest(
            uiBaseUrl = "https://example.com/ui/".toHttpUrl(),
            refreshToken = "grant-secret",
            dpopProvider = dpop
        )

        val expectedUrl = "https://example.com/api/v3/auth/device/refresh"
        assertEquals(expectedUrl, request.url.toString())
        assertEquals("test-dpop-proof", request.header("DPoP"))
        assertEquals("https://example.com", request.header("Origin"))
        assertEquals("https://example.com/ui/", request.header("Referer"))
        assertEquals("POST", dpop.proofMethod)
        assertEquals(expectedUrl, dpop.proofUrl)

        // No `ath`: the refresh is authenticated by the token in the body plus
        // possession of the device key, and binding it to an access token the
        // caller may no longer hold would make refresh fail exactly when it is
        // needed.
        assertNull(dpop.proofAccessToken)

        val buffer = Buffer()
        request.body?.writeTo(buffer)
        val bodyJson = JSONObject(buffer.readUtf8())
        assertEquals("grant-secret", bodyJson.getString("refresh_token"))
    }

    @Test
    fun `refreshSession parses contract DeviceGrantResponse correctly`() = runBlocking {
        val dpop = CapturingDPoPProvider()
        val jsonResponse = """
            {
                "access_token": "at_new_token_123",
                "device_id": "dev_456",
                "expires_in": 3600,
                "refresh_token": "rt_rotated_789",
                "scope": "device:read",
                "token_type": "DPoP"
            }
        """.trimIndent()

        val okHttpClient = OkHttpClient.Builder().addInterceptor(Interceptor { chain ->
            val request = chain.request()
            assertEquals("https://example.com/api/v3/auth/device/refresh", request.url.toString())
            Response.Builder()
                .request(request)
                .protocol(Protocol.HTTP_1_1)
                .code(200)
                .message("OK")
                .body(jsonResponse.toResponseBody("application/json".toMediaType()))
                .build()
        }).build()

        val transport = NativeDeviceAuthTransport(dpopProvider = dpop, okHttpClient = okHttpClient)
        val session = transport.refreshSession(
            uiBaseUrl = "https://example.com/ui/".toHttpUrl(),
            refreshToken = "rt_current_000"
        )

        assertEquals("at_new_token_123", session.accessToken)
        assertEquals("dev_456", session.deviceId)
        assertEquals("rt_rotated_789", session.rotatedRefreshToken)
        assertEquals("device:read", session.scope)

        // A lifetime, not an instant: resolving it is the caller's job, against
        // the caller's clock.
        assertEquals(3600, session.expiresInSeconds)
    }

    @Test(expected = IOException::class)
    fun `refreshSession throws IOException on HTTP error response`() = runBlocking {
        val dpop = CapturingDPoPProvider()
        val errorResponse = """{"title": "Unauthorized", "status": 401}"""

        val okHttpClient = OkHttpClient.Builder().addInterceptor(Interceptor { chain ->
            Response.Builder()
                .request(chain.request())
                .protocol(Protocol.HTTP_1_1)
                .code(401)
                .message("Unauthorized")
                .body(errorResponse.toResponseBody("application/json".toMediaType()))
                .build()
        }).build()

        val transport = NativeDeviceAuthTransport(dpopProvider = dpop, okHttpClient = okHttpClient)
        transport.refreshSession(
            uiBaseUrl = "https://example.com/ui/".toHttpUrl(),
            refreshToken = "rt_current_000"
        )
        Unit
    }
}
