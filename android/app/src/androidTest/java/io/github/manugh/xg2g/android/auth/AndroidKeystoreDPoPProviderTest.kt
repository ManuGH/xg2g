package io.github.manugh.xg2g.android.auth

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.security.KeyStore
import java.security.Signature
import java.util.Base64
import java.util.concurrent.TimeUnit

/**
 * Covers the production signing path that JVM unit tests cannot reach: a hardware/TEE-backed
 * AndroidKeyStore key, its provider's `Signature.sign()`, and the DER -> JOSE conversion on top.
 *
 * The unit tests in src/test exercise [ES256Signature] and [SoftwareDPoPProvider] on a plain JCA
 * key. Only this test proves the same holds for [AndroidKeystoreDPoPProvider], whose key never
 * leaves the keystore and whose signature comes from a different JCA provider entirely.
 *
 * `backendAcceptsKeystoreProof` additionally verifies the proof against a real running backend.
 * It is skipped unless a URL is supplied:
 *
 *   ./gradlew :app:connectedDevDebugAndroidTest \
 *     -Pandroid.testInstrumentationRunnerArguments.backendUrl=http://10.0.2.2:8080
 */
@RunWith(AndroidJUnit4::class)
class AndroidKeystoreDPoPProviderTest {

    private val testAlias = "xg2g_dpop_instrumentation_test_key"
    private val provider = AndroidKeystoreDPoPProvider(keyAlias = testAlias)

    @After
    fun deleteTestKey() {
        val ks = KeyStore.getInstance("AndroidKeyStore")
        ks.load(null)
        if (ks.containsAlias(testAlias)) {
            ks.deleteEntry(testAlias)
        }
    }

    @Test
    fun keystoreProofSignatureIsRaw64BytesAndVerifies() {
        // Repeat: a short R or S occurs in roughly 1 of 256 signatures and must still pad to 32.
        repeat(64) {
            val proof = provider.createProof("GET", "https://xg2g.example/api/v3/dashboard", "at_test_token")
            val segments = proof.split(".")
            assertEquals("proof must be a three-segment JWT", 3, segments.size)

            val signature = Base64.getUrlDecoder().decode(segments[2])
            // Length is the discriminator: DER for P-256 is 68-72 bytes and can never be 64.
            // The leading byte is the top byte of R and may legitimately be anything, 0x30 included.
            assertEquals(
                "AndroidKeyStore ES256 signature must be raw R||S (first byte 0x%02x)".format(signature[0]),
                64,
                signature.size
            )

            val verifier = Signature.getInstance("SHA256withECDSA")
            verifier.initVerify(provider.getOrGenerateKeyPair().public)
            verifier.update("${segments[0]}.${segments[1]}".toByteArray(Charsets.UTF_8))
            assertTrue(
                "signature must verify against the keystore public key",
                verifier.verify(rawToDer(signature))
            )
        }
    }

    /**
     * Drives a keystore-signed proof through the backend's real `ValidateProof`.
     *
     * DeviceGrantFinish validates the DPoP proof before it parses the request body, so an
     * intentionally malformed body separates the two outcomes without needing a passkey or any
     * credential: a rejected proof answers `auth/invalid_dpop`, an accepted one gets far enough
     * to complain about the body instead. The DER control case proves the probe discriminates.
     */
    @Test
    fun backendAcceptsKeystoreProof() {
        val baseUrl = InstrumentationRegistry.getArguments().getString("backendUrl")
        assumeTrue("backendUrl instrumentation argument not supplied", !baseUrl.isNullOrBlank())

        val endpoint = baseUrl!!.trimEnd('/') + "/api/v3/auth/device/grant/finish"
        val client = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            .readTimeout(10, TimeUnit.SECONDS)
            .build()

        val proof = provider.createProof("POST", endpoint)
        val accepted = postProof(client, endpoint, proof)
        assertFalse(
            "endpoint is not routed (HTTP 404). The device grant routes exist in mountPasskeyRoutes, " +
                "which only v3.NewHandler builds; the daemon wires routes through " +
                "RegisterPasskeyRoutesWithRegistrar, which does not register them.",
            accepted.first == 404
        )
        assertFalse(
            "backend rejected a keystore-signed proof: ${accepted.second}",
            accepted.second.contains("invalid_dpop")
        )

        // Control: the same proof re-encoded as DER (the pre-fix wire format) must be rejected,
        // otherwise the assertion above would pass for the wrong reason.
        val segments = proof.split(".")
        val derSignature = rawToDer(Base64.getUrlDecoder().decode(segments[2]))
        val derProof = "${segments[0]}.${segments[1]}." +
            Base64.getUrlEncoder().withoutPadding().encodeToString(derSignature)
        val rejected = postProof(client, endpoint, derProof)
        assertTrue(
            "backend accepted a DER-encoded proof; the probe does not discriminate: ${rejected.second}",
            rejected.second.contains("invalid_dpop")
        )
    }

    private fun postProof(client: OkHttpClient, endpoint: String, proof: String): Pair<Int, String> {
        val url = endpoint.toHttpUrl()
        val origin = "${url.scheme}://${url.host}:${url.port}"

        // Deliberately malformed body: we are probing proof validation, which runs first.
        // Origin is required by the backend's CSRF middleware on unsafe methods, which rejects
        // with 403 CSRF_FORBIDDEN before any DPoP validation happens.
        val request = Request.Builder()
            .url(endpoint)
            .header("DPoP", proof)
            .header("Origin", origin)
            .post("not-json".toRequestBody("application/json".toMediaType()))
            .build()

        client.newCall(request).execute().use { response ->
            val body = response.body?.string().orEmpty()
            val problemType = runCatching { JSONObject(body).optString("type") }.getOrDefault("")
            return response.code to (problemType.ifEmpty { body })
        }
    }

    /** JCA's verifier and the DER control case both need the ASN.1 form back. */
    private fun rawToDer(raw: ByteArray): ByteArray {
        val r = derInteger(trimLeadingZeros(raw.copyOfRange(0, 32)))
        val s = derInteger(trimLeadingZeros(raw.copyOfRange(32, 64)))
        return byteArrayOf(0x30, (r.size + s.size).toByte()) + r + s
    }

    private fun derInteger(magnitude: ByteArray): ByteArray {
        val content = if (magnitude[0].toInt() and 0x80 != 0) byteArrayOf(0x00) + magnitude else magnitude
        return byteArrayOf(0x02, content.size.toByte()) + content
    }

    private fun trimLeadingZeros(bytes: ByteArray): ByteArray {
        var start = 0
        while (start < bytes.size - 1 && bytes[start].toInt() == 0) start++
        return bytes.copyOfRange(start, bytes.size)
    }
}
