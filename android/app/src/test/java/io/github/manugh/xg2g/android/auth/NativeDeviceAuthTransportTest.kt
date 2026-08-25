package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.transport.auth.buildNativeDeviceSessionRequest
import java.security.KeyPair
import java.security.KeyPairGenerator
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Test

class NativeDeviceAuthTransportTest {
    private class CapturingDPoPProvider : DPoPProvider {
        var proofMethod: String? = null
        var proofUrl: String? = null

        override fun getOrGenerateKeyPair(): KeyPair =
            KeyPairGenerator.getInstance("EC").apply { initialize(256) }.generateKeyPair()

        override fun getJWKThumbprint(): String = "test-jkt"

        override fun createProof(htm: String, htu: String, accessToken: String?): String {
            proofMethod = htm
            proofUrl = htu
            return "test-dpop-proof"
        }
    }

    @Test
    fun `refresh roots API URL at origin and signs the transported URL`() {
        val dpop = CapturingDPoPProvider()
        val request = buildNativeDeviceSessionRequest(
            uiBaseUrl = "https://example.com/ui/".toHttpUrl(),
            deviceGrantId = "grant-id",
            deviceGrant = "grant-secret",
            dpopProvider = dpop
        )

        val expectedUrl = "https://example.com/api/v3/auth/device/session"
        assertEquals(expectedUrl, request.url.toString())
        assertEquals("test-dpop-proof", request.header("DPoP"))
        assertEquals("https://example.com", request.header("Origin"))
        assertEquals("https://example.com/ui/", request.header("Referer"))
        assertEquals("POST", dpop.proofMethod)
        assertEquals(expectedUrl, dpop.proofUrl)
    }
}
