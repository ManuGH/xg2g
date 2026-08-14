package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.IOException
import java.security.KeyPair
import java.security.KeyPairGenerator

class NativeDPoPInterceptorTest {

    private class FakeStateStore(private var state: PersistedDeviceAuthState? = null) : PersistedDeviceAuthStateStore {
        override fun load(): PersistedDeviceAuthState? = state
        override fun save(state: PersistedDeviceAuthState) { this.state = state }
        override fun clear() { state = null }
    }

    private class FakeDPoPProvider : DPoPProvider {
        override fun createProof(htm: String, htu: String, accessToken: String?): String {
            return "fake_dpop_proof_for_$htm"
        }

        override fun getOrGenerateKeyPair(): KeyPair {
            val keyPairGenerator = KeyPairGenerator.getInstance("EC")
            keyPairGenerator.initialize(256)
            return keyPairGenerator.generateKeyPair()
        }

        override fun getJWKThumbprint(): String = "fake_jkt_thumbprint"
    }

    @Test
    fun `401 with invalid_grant triggers ReauthRequired not Revoked`() {
        val stateStore = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "http://127.0.0.1:8080",
                deviceGrantId = "dg_123",
                deviceGrant = "grant_abc",
                accessToken = "at_invalid_123"
            )
        )
        val dpopProvider = FakeDPoPProvider()
        val stateMachine = AuthStateMachine(isTvDevice = true)
        stateMachine.activateDeviceGrant("dg_123", "at_invalid_123", "jkt_123", System.currentTimeMillis() + 3600000)

        val interceptor = NativeDPoPInterceptor(stateStore, dpopProvider, stateMachine)

        val mockClient = OkHttpClient.Builder()
            .addInterceptor(interceptor)
            .addInterceptor { chain ->
                Response.Builder()
                    .request(chain.request())
                    .protocol(Protocol.HTTP_1_1)
                    .code(401)
                    .message("Unauthorized")
                    .body("""{"error": "invalid_grant", "error_description": "Token has expired or key mismatch"}""".toResponseBody("application/json".toMediaType()))
                    .build()
            }
            .build()

        val request = Request.Builder().url("http://127.0.0.1:8080/api/v3/dashboard").build()
        val response = mockClient.newCall(request).execute()

        assertEquals(401, response.code)
        assertEquals(AuthStateKind.ReauthRequired, stateMachine.currentState.kind)
    }

    @Test
    fun `401 with explicit device_revoked triggers Revoked`() {
        val stateStore = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "http://127.0.0.1:8080",
                deviceGrantId = "dg_123",
                deviceGrant = "grant_abc",
                accessToken = "at_revoked_123"
            )
        )
        val dpopProvider = FakeDPoPProvider()
        val stateMachine = AuthStateMachine(isTvDevice = true)
        stateMachine.activateDeviceGrant("dg_123", "at_revoked_123", "jkt_123", System.currentTimeMillis() + 3600000)

        val interceptor = NativeDPoPInterceptor(stateStore, dpopProvider, stateMachine)

        val mockClient = OkHttpClient.Builder()
            .addInterceptor(interceptor)
            .addInterceptor { chain ->
                Response.Builder()
                    .request(chain.request())
                    .protocol(Protocol.HTTP_1_1)
                    .code(401)
                    .message("Unauthorized")
                    .body("""{"error": "device_revoked", "error_description": "Device has been disabled by admin"}""".toResponseBody("application/json".toMediaType()))
                    .build()
            }
            .build()

        val request = Request.Builder().url("http://127.0.0.1:8080/api/v3/dashboard").build()
        val response = mockClient.newCall(request).execute()

        assertEquals(401, response.code)
        assertEquals(AuthStateKind.Revoked, stateMachine.currentState.kind)
    }

    @Test
    fun `network IOException does NOT transition state to Revoked or ReauthRequired`() {
        val stateStore = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "http://127.0.0.1:8080",
                deviceGrantId = "dg_123",
                deviceGrant = "grant_abc",
                accessToken = "at_valid_123"
            )
        )
        val dpopProvider = FakeDPoPProvider()
        val stateMachine = AuthStateMachine(isTvDevice = true)
        stateMachine.activateDeviceGrant("dg_123", "at_valid_123", "jkt_123", System.currentTimeMillis() + 3600000)

        val interceptor = NativeDPoPInterceptor(stateStore, dpopProvider, stateMachine)

        val mockClient = OkHttpClient.Builder()
            .addInterceptor(interceptor)
            .addInterceptor { _ ->
                throw IOException("Network connection lost")
            }
            .build()

        val request = Request.Builder().url("http://127.0.0.1:8080/api/v3/dashboard").build()

        try {
            mockClient.newCall(request).execute()
        } catch (_: IOException) {
            // Expected
        }

        assertEquals(AuthStateKind.DeviceGrantActive, stateMachine.currentState.kind)
    }
}
