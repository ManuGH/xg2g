package io.github.manugh.xg2g.android.pairing

import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

internal data class StartPairingResult(
    val pairingId: String,
    val pairingSecret: String,
    val userCode: String,
    val qrPayload: String,
    val expiresAt: String
)

internal data class PairingStatusResult(
    val pairingId: String,
    val status: String,
    val userCode: String,
    val approvedAt: String?,
    val expiresAt: String
)

internal data class ExchangePairingResult(
    val pairingId: String,
    val deviceId: String,
    val accessToken: String,
    val accessTokenExpiresAt: String
)

internal class PairingApiClient(
    private val baseUrl: String,
    private val okHttpClient: OkHttpClient = OkHttpClient()
) {
    suspend fun startPairing(
        deviceName: String = "Android TV",
        deviceType: String = "tv"
    ): StartPairingResult = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            throw IllegalStateException("Server-URL ist nicht konfiguriert.")
        }
        val url = apiUrl("pairing", "start")
        val json = JSONObject().apply {
            put("deviceName", deviceName)
            put("deviceType", deviceType)
            put("requestedPolicyProfile", "native-app")
        }
        val request = Request.Builder()
            .url(url)
            .post(json.toString().toRequestBody(JSON_MEDIA_TYPE))
            .build()
            .withSameOriginHeaders(url)

        val response = okHttpClient.newCall(request).execute()
        val bodyStr = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            throw IllegalStateException("Pairing start failed (HTTP ${response.code}): $bodyStr")
        }

        val obj = JSONObject(bodyStr)
        StartPairingResult(
            pairingId = obj.getString("pairingId"),
            pairingSecret = obj.getString("pairingSecret"),
            userCode = obj.getString("userCode"),
            qrPayload = obj.optString("qrPayload"),
            expiresAt = obj.optString("expiresAt")
        )
    }

    suspend fun getPairingStatus(
        pairingId: String,
        pairingSecret: String
    ): PairingStatusResult = withContext(Dispatchers.IO) {
        val url = apiUrl("pairing", pairingId, "status")
        val json = JSONObject().apply {
            put("pairingSecret", pairingSecret)
        }
        val request = Request.Builder()
            .url(url)
            .post(json.toString().toRequestBody(JSON_MEDIA_TYPE))
            .build()
            .withSameOriginHeaders(url)

        val response = okHttpClient.newCall(request).execute()
        val bodyStr = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            throw IllegalStateException("Pairing status failed (HTTP ${response.code}): $bodyStr")
        }

        val obj = JSONObject(bodyStr)
        PairingStatusResult(
            pairingId = obj.getString("pairingId"),
            status = obj.getString("status"),
            userCode = obj.optString("userCode"),
            approvedAt = obj.optString("approvedAt").takeIf { it.isNotEmpty() },
            expiresAt = obj.optString("expiresAt")
        )
    }

    suspend fun exchangePairing(
        pairingId: String,
        pairingSecret: String
    ): ExchangePairingResult = withContext(Dispatchers.IO) {
        val url = apiUrl("pairing", pairingId, "exchange")
        val json = JSONObject().apply {
            put("pairingSecret", pairingSecret)
        }
        val request = Request.Builder()
            .url(url)
            .post(json.toString().toRequestBody(JSON_MEDIA_TYPE))
            .build()
            .withSameOriginHeaders(url)

        val response = okHttpClient.newCall(request).execute()
        val bodyStr = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            throw IllegalStateException("Pairing exchange failed (HTTP ${response.code}): $bodyStr")
        }

        val obj = JSONObject(bodyStr)
        ExchangePairingResult(
            pairingId = obj.getString("pairingId"),
            deviceId = obj.optString("deviceId"),
            accessToken = obj.getString("accessToken"),
            accessTokenExpiresAt = obj.optString("accessTokenExpiresAt")
        )
    }

    private fun apiUrl(vararg segments: String): HttpUrl {
        val parsed = baseUrl.toHttpUrlOrNull()
            ?: throw IllegalArgumentException("Invalid server base URL: $baseUrl")
        val builder = parsed.newBuilder()
            .encodedPath("/api/v3/")
            .query(null)
            .fragment(null)
        for (segment in segments) {
            builder.addPathSegment(segment)
        }
        return builder.build()
    }

    companion object {
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
