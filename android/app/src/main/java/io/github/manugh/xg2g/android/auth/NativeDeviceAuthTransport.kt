package io.github.manugh.xg2g.android.auth

import android.util.Log
import io.github.manugh.xg2g.android.CompletedWebBootstrap
import io.github.manugh.xg2g.android.DeviceAuthTransport
import io.github.manugh.xg2g.android.PublishedEndpoint
import io.github.manugh.xg2g.android.RefreshedDeviceSession
import io.github.manugh.xg2g.android.StartedWebBootstrap
import io.github.manugh.xg2g.android.apiV3Url
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.io.IOException

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
        Log.i(TAG, "action=refresh_session path=/api/v3/auth/device/session")
        val request = buildNativeDeviceSessionRequest(uiBaseUrl, deviceGrantId, deviceGrant, dpopProvider)
        okHttpClient.newCall(request).execute().use { response ->
            val body = response.body?.string() ?: ""
            if (!response.isSuccessful) {
                throw IOException("Refresh session failed with HTTP ${response.code}: $body")
            }
            val json = JSONObject(body)
            val expiresSec = json.optLong("expiresInSeconds", 86400L)
            val nowMs = System.currentTimeMillis()

            val endpoints = mutableListOf<PublishedEndpoint>()
            val epArray = json.optJSONArray("publishedEndpoints")
            if (epArray != null) {
                for (i in 0 until epArray.length()) {
                    val item = epArray.optJSONObject(i) ?: continue
                    endpoints.add(
                        PublishedEndpoint(
                            url = item.optString("url"),
                            kind = item.optString("kind"),
                            priority = item.optInt("priority"),
                            tlsMode = item.optString("tlsMode"),
                            allowPairing = item.optBoolean("allowPairing"),
                            allowStreaming = item.optBoolean("allowStreaming"),
                            allowWeb = item.optBoolean("allowWeb"),
                            allowNative = item.optBoolean("allowNative"),
                            advertiseReason = item.optString("advertiseReason"),
                            source = item.optString("source", "config")
                        )
                    )
                }
            }

            RefreshedDeviceSession(
                accessSessionId = json.optString("accessSessionId", ""),
                accessToken = json.optString("accessToken", ""),
                accessTokenExpiresAtEpochMs = nowMs + (expiresSec * 1000L),
                rotatedDeviceGrantId = json.optString("rotatedDeviceGrantId").takeIf { !it.isNullOrBlank() },
                rotatedDeviceGrant = json.optString("rotatedDeviceGrant").takeIf { !it.isNullOrBlank() },
                policyVersion = json.optString("policyVersion").takeIf { !it.isNullOrBlank() },
                endpoints = endpoints
            )
        }
    }

    override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {
        // No-op for Native Device Auth. WebUI cookie adapter handles WebView sessions separately.
    }

    override suspend fun startWebBootstrap(uiBaseUrl: HttpUrl, accessToken: String, targetPath: String): StartedWebBootstrap {
        return StartedWebBootstrap(completePath = targetPath, bootstrapToken = "")
    }

    override suspend fun completeWebBootstrap(uiBaseUrl: HttpUrl, completePath: String, bootstrapToken: String): CompletedWebBootstrap {
        return CompletedWebBootstrap(locationPath = completePath)
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
    val refreshUrl = apiV3Url(uiBaseUrl, "auth", "device", "session")
    val jsonBody = JSONObject()
        .put("deviceGrantId", deviceGrantId)
        .put("deviceGrant", deviceGrant)
        .toString()
        .toRequestBody("application/json; charset=utf-8".toMediaType())

    return Request.Builder()
        .url(refreshUrl)
        .post(jsonBody)
        .header("DPoP", dpopProvider.createProof("POST", refreshUrl.toString()))
        .build()
        .withSameOriginHeaders(uiBaseUrl)
}
