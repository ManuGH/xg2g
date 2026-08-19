package io.github.manugh.xg2g.android.auth

import android.util.Log
import io.github.manugh.xg2g.android.CompletedWebBootstrap
import io.github.manugh.xg2g.android.DeviceAuthTransport
import io.github.manugh.xg2g.android.RefreshedDeviceSession
import io.github.manugh.xg2g.android.StartedWebBootstrap
import io.github.manugh.xg2g.android.apiV3Url
import io.github.manugh.xg2g.android.contract.DeviceGrantResponse
import io.github.manugh.xg2g.android.contract.DeviceRefreshRequest
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
        refreshToken: String
    ): RefreshedDeviceSession = withContext(Dispatchers.IO) {
        Log.i(TAG, "action=refresh_session path=/api/v3/auth/device/refresh")
        val request = buildDeviceRefreshRequest(uiBaseUrl, refreshToken, dpopProvider)
        okHttpClient.newCall(request).execute().use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw IOException("Refresh session failed with HTTP ${response.code}: $body")
            }
            refreshedSessionFrom(JSONObject(body))
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
