package io.github.manugh.xg2g.android.auth

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONObject
import java.security.KeyFactory
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.Signature
import java.security.interfaces.ECPublicKey
import java.security.spec.ECPoint
import java.util.UUID

interface DPoPProvider {
    fun getOrGenerateKeyPair(): KeyPair
    fun getJWKThumbprint(): String
    fun createProof(htm: String, htu: String, accessToken: String? = null): String
}

class AndroidKeystoreDPoPProvider(
    private val keyAlias: String = "xg2g_dpop_device_key"
) : DPoPProvider {

    override fun getOrGenerateKeyPair(): KeyPair {
        val ks = KeyStore.getInstance("AndroidKeyStore")
        ks.load(null)

        if (ks.containsAlias(keyAlias)) {
            val entry = ks.getEntry(keyAlias, null) as? KeyStore.PrivateKeyEntry
            if (entry != null) {
                return KeyPair(entry.certificate.publicKey, entry.privateKey)
            }
        }

        val kpg = KeyPairGenerator.getInstance(
            KeyProperties.KEY_ALGORITHM_EC,
            "AndroidKeyStore"
        )
        val spec = KeyGenParameterSpec.Builder(
            keyAlias,
            KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY
        )
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setAlgorithmParameterSpec(java.security.spec.ECGenParameterSpec("secp256r1"))
            .build()

        kpg.initialize(spec)
        return kpg.generateKeyPair()
    }

    override fun getJWKThumbprint(): String {
        val keyPair = getOrGenerateKeyPair()
        val ecPubKey = keyPair.public as ECPublicKey
        return computeJWKThumbprint(ecPubKey)
    }

    override fun createProof(htm: String, htu: String, accessToken: String?): String {
        val keyPair = getOrGenerateKeyPair()
        val ecPubKey = keyPair.public as ECPublicKey
        val jkt = computeJWKThumbprint(ecPubKey)

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
        // JCA returns ASN.1 DER; JWS ES256 requires raw R || S (RFC 7518 §3.4).
        val sigBytes = ES256Signature.derToRaw(signature.sign())
        val sigB64 = base64UrlEncode(sigBytes)

        return "$signingInput.$sigB64"
    }

    companion object {
        fun computeJWKThumbprint(pubKey: ECPublicKey): String {
            val point = pubKey.w
            val xB64 = base64UrlEncode(bigIntToFixedByteArray(point.affineX, 32))
            val yB64 = base64UrlEncode(bigIntToFixedByteArray(point.affineY, 32))

            val canonicalJwk = "{\"crv\":\"P-256\",\"kty\":\"EC\",\"x\":\"$xB64\",\"y\":\"$yB64\"}"
            val digest = MessageDigest.getInstance("SHA-256")
            val hash = digest.digest(canonicalJwk.toByteArray(Charsets.UTF_8))
            return base64UrlEncode(hash)
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
            return Base64.encodeToString(bytes, Base64.NO_WRAP or Base64.NO_PADDING or Base64.URL_SAFE)
        }
    }
}
