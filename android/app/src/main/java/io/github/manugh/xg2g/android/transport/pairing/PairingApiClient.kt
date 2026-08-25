package io.github.manugh.xg2g.android.transport.pairing

import io.github.manugh.xg2g.android.ServerEndpoint
import io.github.manugh.xg2g.android.contract.PublishedEndpoint
import io.github.manugh.xg2g.android.transport.apiV3Url
import io.github.manugh.xg2g.android.transport.parseServerEndpoints
import io.github.manugh.xg2g.android.transport.playback.withSameOriginHeaders
import java.time.Instant
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
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
    val deviceGrantId: String,
    val deviceGrant: String,
    val accessSessionId: String?,
    val accessToken: String,
    val accessTokenExpiresAtEpochMs: Long,
    val policyVersion: String?,
    val endpoints: List<ServerEndpoint>
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
        val expiresAtStr = obj.optString("accessTokenExpiresAt")
        val expiresAtEpochMs = parseHttpInstant(expiresAtStr) ?: (System.currentTimeMillis() + 900_000L)

        ExchangePairingResult(
            pairingId = obj.getString("pairingId"),
            deviceId = obj.optString("deviceId"),
            deviceGrantId = obj.getString("deviceGrantId"),
            deviceGrant = obj.getString("deviceGrant"),
            accessSessionId = obj.optString("accessSessionId").takeIf { it.isNotBlank() },
            accessToken = obj.getString("accessToken"),
            accessTokenExpiresAtEpochMs = expiresAtEpochMs,
            policyVersion = obj.optString("policyVersion").takeIf { it.isNotBlank() },
            endpoints = parseEndpoints(obj.optJSONArray("endpoints"))
        )
    }

    private fun parseEndpoints(array: JSONArray?): List<ServerEndpoint> =
        parseServerEndpoints(array)

    private fun parseHttpInstant(value: String?): Long? {
        val trimmed = value?.trim()?.takeIf { it.isNotEmpty() } ?: return null
        return trimmed.toLongOrNull()?.takeIf { it > 0L }
            ?: runCatching {
                Instant.from(DateTimeFormatter.RFC_1123_DATE_TIME.parse(trimmed)).toEpochMilli()
            }.getOrNull()
            ?: runCatching {
                Instant.parse(trimmed).toEpochMilli()
            }.getOrNull()
    }

    private fun apiUrl(vararg segments: String): HttpUrl {
        val parsed = baseUrl.toHttpUrlOrNull()
            ?: throw IllegalArgumentException("Invalid server base URL: $baseUrl")
        return apiV3Url(parsed, *segments)
    }

    companion object {
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
