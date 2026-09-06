package io.github.manugh.xg2g.android.transport.auth

import android.util.Log
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.contract.PublishedEndpoint
import io.github.manugh.xg2g.android.transport.DeviceAuthTransport
import io.github.manugh.xg2g.android.transport.RefreshedDeviceSession
import io.github.manugh.xg2g.android.transport.apiV3Url
import io.github.manugh.xg2g.android.transport.parseServerEndpoints
import io.github.manugh.xg2g.android.transport.playback.withSameOriginHeaders
import java.io.IOException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

internal class NativeDeviceAuthTransport(
    private val dpopProvider: DPoPProvider,
    private val okHttpClient: OkHttpClient = OkHttpClient.Builder()
        .followRedirects(false)
        .followSslRedirects(false)
        .build()
) : DeviceAuthTransport {

    override suspend fun refreshSession(
        uiBaseUrl: HttpUrl,
        deviceGrantId: String,
        deviceGrant: String
    ): RefreshedDeviceSession = withContext(Dispatchers.IO) {
        Log.i(TAG, "action=refresh_session path=/api/v3/auth/device/refresh")
        val request = buildNativeDeviceSessionRequest(uiBaseUrl, deviceGrantId, deviceGrant, dpopProvider)
        okHttpClient.newCall(request).execute().use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw IOException("Refresh session failed with HTTP ${response.code}: $body")
            }
            val json = JSONObject(body)
            val expiresSec = if (json.has("expires_in")) {
                json.optLong("expires_in", 86400L)
            } else {
                json.optLong("expiresInSeconds", 86400L)
            }
            val nowMs = System.currentTimeMillis()

            val endpoints = parseServerEndpoints(json.optJSONArray("publishedEndpoints"))
            val accessToken = if (json.has("access_token")) {
                json.optString("access_token", "")
            } else {
                json.optString("accessToken", "")
            }
            val rotatedGrant = if (json.has("refresh_token")) {
                json.optString("refresh_token")
            } else {
                json.optString("rotatedDeviceGrant")
            }.takeIf { !it.isNullOrBlank() }

            val deviceId = if (json.has("device_id")) {
                json.optString("device_id")
            } else {
                json.optString("rotatedDeviceGrantId")
            }.takeIf { !it.isNullOrBlank() }

            val sessionId = if (json.has("accessSessionId")) {
                json.optString("accessSessionId", "")
            } else {
                json.optString("device_id", "")
            }

            RefreshedDeviceSession(
                accessSessionId = sessionId,
                accessToken = accessToken,
                accessTokenExpiresAtEpochMs = nowMs + (expiresSec * 1000L),
                rotatedDeviceGrantId = deviceId,
                rotatedDeviceGrant = rotatedGrant,
                policyVersion = json.optString("policyVersion").takeIf { !it.isNullOrBlank() },
                endpoints = endpoints
            )
        }
    }

    override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {
        // No-op for Native Device Auth. WebUI cookie adapter handles WebView sessions separately.
    }

    private companion object {
        const val TAG = "NativeDeviceAuthTransport"
    }
}

internal fun buildNativeDeviceSessionRequest(
    uiBaseUrl: HttpUrl,
    deviceGrantId: String,
    deviceGrant: String,
    dpopProvider: DPoPProvider
): Request {
    val refreshUrl = apiV3Url(uiBaseUrl, "auth", "device", "refresh")
    val jsonBody = JSONObject()
        .put("refresh_token", deviceGrant)
        .toString()
        .toRequestBody("application/json; charset=utf-8".toMediaType())

    return Request.Builder()
        .url(refreshUrl)
        .post(jsonBody)
        .header("DPoP", dpopProvider.createProof("POST", refreshUrl.toString()))
        .build()
        .withSameOriginHeaders(uiBaseUrl)
}
