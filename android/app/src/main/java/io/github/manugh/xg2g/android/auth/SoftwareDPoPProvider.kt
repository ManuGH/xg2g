package io.github.manugh.xg2g.android.auth

import org.json.JSONObject
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.security.Signature
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec
import java.util.Base64
import java.util.UUID

class SoftwareDPoPProvider : DPoPProvider {
    private val keyPair: KeyPair

    init {
        val kpg = KeyPairGenerator.getInstance("EC")
        kpg.initialize(ECGenParameterSpec("secp256r1"))
        keyPair = kpg.generateKeyPair()
    }

    override fun getOrGenerateKeyPair(): KeyPair = keyPair

    override fun getJWKThumbprint(): String {
        val ecPubKey = keyPair.public as ECPublicKey
        val point = ecPubKey.w
        val xB64 = base64UrlEncode(bigIntToFixedByteArray(point.affineX, 32))
        val yB64 = base64UrlEncode(bigIntToFixedByteArray(point.affineY, 32))

        val canonicalJwk = "{\"crv\":\"P-256\",\"kty\":\"EC\",\"x\":\"$xB64\",\"y\":\"$yB64\"}"
        val digest = MessageDigest.getInstance("SHA-256")
        val hash = digest.digest(canonicalJwk.toByteArray(Charsets.UTF_8))
        return base64UrlEncode(hash)
    }

    override fun createProof(htm: String, htu: String, accessToken: String?): String {
        val ecPubKey = keyPair.public as ECPublicKey
        val jkt = getJWKThumbprint()

        val point = ecPubKey.w
        val xB64 = base64UrlEncode(bigIntToFixedByteArray(point.affineX, 32))
        val yB64 = base64UrlEncode(bigIntToFixedByteArray(point.affineY, 32))

        val headerObj = JSONObject().apply {
            put("typ", "dpop+jwt")
            put("alg", "ES256")
            put("jwk", JSONObject().apply {
                put("kty", "EC")
                put("crv", "P-256")
                put("x", xB64)
                put("y", yB64)
            })
        }

        val payloadObj = JSONObject().apply {
            put("jti", UUID.randomUUID().toString())
            put("htm", htm.uppercase())
            put("htu", htu)
            put("iat", System.currentTimeMillis() / 1000)
            if (!accessToken.isNullOrEmpty()) {
                val digest = MessageDigest.getInstance("SHA-256")
                val athBytes = digest.digest(accessToken.toByteArray(Charsets.UTF_8))
                put("ath", base64UrlEncode(athBytes))
            }
        }

        val headerB64 = base64UrlEncode(headerObj.toString().toByteArray(Charsets.UTF_8))
        val payloadB64 = base64UrlEncode(payloadObj.toString().toByteArray(Charsets.UTF_8))
        val signingInput = "$headerB64.$payloadB64"

        val signature = Signature.getInstance("SHA256withECDSA")
        signature.initSign(keyPair.private)
        signature.update(signingInput.toByteArray(Charsets.UTF_8))
        val sigBytes = signature.sign()
        val sigB64 = base64UrlEncode(sigBytes)

        return "$signingInput.$sigB64"
    }

    private fun bigIntToFixedByteArray(bi: java.math.BigInteger, length: Int): ByteArray {
        val array = bi.toByteArray()
        if (array.size == length) return array
        if (array.size > length) {
            val dest = ByteArray(length)
            System.arraycopy(array, array.size - length, dest, 0, length)
            return dest
        }
        val dest = ByteArray(length)
        System.arraycopy(array, 0, dest, length - array.size, array.size)
        return dest
    }

    private fun base64UrlEncode(bytes: ByteArray): String {
        return Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
    }
}
