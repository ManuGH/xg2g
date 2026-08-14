package io.github.manugh.xg2g.android.auth

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.Signature
import java.security.interfaces.ECPublicKey
import java.util.Base64

/**
 * The backend (`verifyES256Signature` in backend/internal/control/http/v3/dpop/dpop.go) rejects
 * any DPoP proof whose signature is not exactly 64 raw bytes, per JWS ES256 (RFC 7518 §3.4).
 * JCA hands us ASN.1 DER instead, so every proof must be converted before it goes on the wire.
 */
class DPoPProofSignatureTest {

    @Test
    fun `proof signature segment is exactly 64 bytes and verifies against the public key`() {
        val provider = SoftwareDPoPProvider()
        val signingInputsSeen = mutableSetOf<String>()

        // ~1 in 256 signatures has a short R or S; repeat so the padding path is actually exercised.
        repeat(256) {
            val proof = provider.createProof(
                htm = "GET",
                htu = "https://xg2g.example/api/v3/dashboard",
                accessToken = "at_test_token"
            )

            val segments = proof.split(".")
            assertEquals("proof must be a three-segment JWT", 3, segments.size)
            signingInputsSeen += "${segments[0]}.${segments[1]}"

            val rawSignature = Base64.getUrlDecoder().decode(segments[2])
            assertEquals(
                "ES256 signature must be raw R||S, not DER (first byte 0x%02x)".format(rawSignature[0]),
                64,
                rawSignature.size
            )

            val verifier = Signature.getInstance("SHA256withECDSA")
            verifier.initVerify(provider.getOrGenerateKeyPair().public)
            verifier.update("${segments[0]}.${segments[1]}".toByteArray(Charsets.UTF_8))
            assertTrue("signature must verify against the proof's own public key", verifier.verify(rawToDer(rawSignature)))
        }

        assertEquals("each proof must carry a unique jti", 256, signingInputsSeen.size)
    }

    @Test
    fun `proof public key matches the advertised jwk thumbprint`() {
        val provider = SoftwareDPoPProvider()
        val proof = provider.createProof("POST", "https://xg2g.example/api/v3/streams", null)
        val header = String(Base64.getUrlDecoder().decode(proof.split(".")[0]), Charsets.UTF_8)

        val ecPubKey = provider.getOrGenerateKeyPair().public as ECPublicKey
        val expectedX = Base64.getUrlEncoder().withoutPadding()
            .encodeToString(fixedWidth(ecPubKey.w.affineX.toByteArray()))

        assertTrue("header jwk must carry the signing key's x coordinate", header.contains("\"x\":\"$expectedX\""))
    }

    @Test
    fun `derToRaw left-pads short components`() {
        // R = 0x01 (1 byte), S = 32 bytes of 0xAA -> needs a leading 0x00 in DER to stay positive.
        val s = ByteArray(32) { 0xAA.toByte() }
        val der = byteArrayOf(0x30, 0x26, 0x02, 0x01, 0x01, 0x02, 0x21, 0x00) + s

        val raw = ES256Signature.derToRaw(der)

        assertEquals(64, raw.size)
        assertArrayEquals(ByteArray(31) + byteArrayOf(0x01), raw.copyOfRange(0, 32))
        assertArrayEquals(s, raw.copyOfRange(32, 64))
    }

    @Test
    fun `derToRaw rejects malformed input`() {
        val notASequence = ByteArray(70) { 0x31 }
        val truncated = byteArrayOf(0x30, 0x44, 0x02, 0x20)
        val rawPassedTwice = ByteArray(64) { 0x07 }

        for (bad in listOf(notASequence, truncated, rawPassedTwice)) {
            try {
                ES256Signature.derToRaw(bad)
                throw AssertionError("expected rejection of malformed DER signature")
            } catch (_: IllegalArgumentException) {
                // Expected: a malformed signature must fail loudly, never silently ship a bad proof.
            }
        }
    }

    private fun fixedWidth(bytes: ByteArray): ByteArray {
        if (bytes.size == 32) return bytes
        val out = ByteArray(32)
        if (bytes.size > 32) {
            System.arraycopy(bytes, bytes.size - 32, out, 0, 32)
        } else {
            System.arraycopy(bytes, 0, out, 32 - bytes.size, bytes.size)
        }
        return out
    }

    /** JCA's verifier expects DER, so convert the wire format back for verification. */
    private fun rawToDer(raw: ByteArray): ByteArray {
        val r = trimLeadingZeros(raw.copyOfRange(0, 32))
        val s = trimLeadingZeros(raw.copyOfRange(32, 64))
        val rDer = derInteger(r)
        val sDer = derInteger(s)
        val body = rDer + sDer
        return byteArrayOf(0x30, body.size.toByte()) + body
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
