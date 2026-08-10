package io.github.manugh.xg2g.android.dashboard

import android.webkit.CookieManager
import io.github.manugh.xg2g.android.DeviceAuthReenrollRequiredException
import io.github.manugh.xg2g.android.DeviceAuthRepository
import io.github.manugh.xg2g.android.DeviceAuthSignInRequiredException
import io.github.manugh.xg2g.android.guide.GuideAuthRequiredException
import io.github.manugh.xg2g.android.guide.GuideHealthStatus
import io.github.manugh.xg2g.android.playback.net.AuthCookieSession
import io.github.manugh.xg2g.android.playback.net.CookieBackedAuthSession
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import org.json.JSONTokener

internal data class DashboardRecordingItem(
    val id: String,
    val title: String,
    val channelName: String?,
    val durationMinutes: Int?,
    val recordedAtEpochSec: Long?
)

internal data class DashboardTimerItem(
    val id: String,
    val title: String,
    val channelName: String?,
    val startEpochSec: Long,
    val state: String
)

internal data class DashboardDvrStatus(
    val diskFreeBytes: Long?,
    val diskTotalBytes: Long?,
    val recordingCount: Int,
    val activeTimerCount: Int
)

internal class DashboardApiClient(
    private val baseUrl: String,
    private val deviceAuthRepository: DeviceAuthRepository? = null,
    private val cookieSession: AuthCookieSession = CookieBackedAuthSession(CookieManager.getInstance()),
    private val okHttpClient: OkHttpClient = OkHttpClient.Builder()
        .addNetworkInterceptor { chain ->
            val original = chain.request()
            val builder = original.newBuilder()
            cookieSession.applyCookies(original.url, builder)
            val response = chain.proceed(builder.build())
            cookieSession.storeCookies(original.url, response.headers)
            response
        }
        .build()
) {
    private suspend fun ensureAuthSession(authToken: String?) {
        withContext(Dispatchers.IO) {
            val sessionUrl = apiUrl("auth", "session")
            val repository = deviceAuthRepository
            if (repository != null) {
                try {
                    repository.ensureAuthSession(baseUrl, authToken)
                    if (cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)) {
                        return@withContext
                    }
                } catch (error: DeviceAuthReenrollRequiredException) {
                    throw GuideAuthRequiredException(410, error.message)
                } catch (error: DeviceAuthSignInRequiredException) {
                    throw GuideAuthRequiredException(401, error.message)
                }
            }

            if (cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)) {
                return@withContext
            }

            val bearerToken = authToken?.trim().takeIf { !it.isNullOrEmpty() } ?: return@withContext
            val request = Request.Builder()
                .url(sessionUrl)
                .header("Authorization", "Bearer $bearerToken")
                .post(ByteArray(0).toRequestBody(null))
                .build()

            execute(request)
        }
    }

    suspend fun fetchHealth(authToken: String?): GuideHealthStatus = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("system", "health")).get()
        authToken?.trim()?.takeIf { it.isNotEmpty() }?.let {
            requestBuilder.header("Authorization", "Bearer $it")
        }
        val root = executeJsonObject(requestBuilder.build())
        val receiverStatus = root.optJSONObject("receiver")
            ?.optString("status")
            ?.lowercase()
            .orEmpty()
        val epgNode = root.optJSONObject("epg")
        val epgStatus = epgNode
            ?.optString("status")
            ?.lowercase()
            .orEmpty()

        GuideHealthStatus(
            receiverHealthy = receiverStatus == "ok",
            epgHealthy = epgStatus == "ok",
            missingChannels = epgNode?.optInt("missingChannels", 0)
        )
    }


    suspend fun fetchRecordings(authToken: String?): List<DashboardRecordingItem> = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("recordings")).get()
        authToken?.trim()?.takeIf { it.isNotEmpty() }?.let {
            requestBuilder.header("Authorization", "Bearer $it")
        }
        val root = execute(requestBuilder.build())
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("recordings") ?: JSONArray()
            else -> JSONArray()
        }
        val items = mutableListOf<DashboardRecordingItem>()
        for (i in 0 until array.length()) {
            val obj = array.optJSONObject(i) ?: continue
            val id = obj.optString("id").ifBlank { obj.optString("recordingId") }
            val title = obj.optString("title").ifBlank { obj.optString("name") }
            if (id.isNotBlank() && title.isNotBlank()) {
                items.add(
                    DashboardRecordingItem(
                        id = id,
                        title = title,
                        channelName = obj.optString("channelName").takeIf { it.isNotBlank() },
                        durationMinutes = obj.optInt("durationMinutes").takeIf { it > 0 },
                        recordedAtEpochSec = obj.optLong("recordedAt").takeIf { it > 0 }
                    )
                )
            }
        }
        items
    }

    suspend fun fetchTimers(authToken: String?): List<DashboardTimerItem> = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("timers")).get()
        authToken?.trim()?.takeIf { it.isNotEmpty() }?.let {
            requestBuilder.header("Authorization", "Bearer $it")
        }
        val root = execute(requestBuilder.build())
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("timers") ?: JSONArray()
            else -> JSONArray()
        }
        val items = mutableListOf<DashboardTimerItem>()
        for (i in 0 until array.length()) {
            val obj = array.optJSONObject(i) ?: continue
            val id = obj.optString("id").ifBlank { obj.optString("timerId") }
            val title = obj.optString("title").ifBlank { obj.optString("name") }
            if (id.isNotBlank() && title.isNotBlank()) {
                items.add(
                    DashboardTimerItem(
                        id = id,
                        title = title,
                        channelName = obj.optString("channelName").takeIf { it.isNotBlank() }
                            ?: obj.optString("serviceName").takeIf { it.isNotBlank() },
                        startEpochSec = obj.optLong("start", obj.optLong("beginUnixSeconds", 0L)),
                        state = obj.optString("state", "active")
                    )
                )
            }
        }
        items
    }

    suspend fun fetchDvrStatus(authToken: String?): DashboardDvrStatus = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("dvr", "status")).get()
        authToken?.trim()?.takeIf { it.isNotEmpty() }?.let {
            requestBuilder.header("Authorization", "Bearer $it")
        }
        val root = executeJsonObject(requestBuilder.build())
        DashboardDvrStatus(
            diskFreeBytes = root.optLong("diskFreeBytes").takeIf { it > 0 },
            diskTotalBytes = root.optLong("diskTotalBytes").takeIf { it > 0 },
            recordingCount = root.optInt("recordingCount", 0),
            activeTimerCount = root.optInt("activeTimerCount", 0)
        )
    }


    private fun executeJsonObject(request: Request): JSONObject {
        val root = execute(request)
        if (root !is JSONObject) {
            throw IllegalStateException("Expected JSON Object")
        }
        return root
    }

    private fun executeJsonArray(request: Request): JSONArray {
        val root = execute(request)
        if (root !is JSONArray) {
            throw IllegalStateException("Expected JSON Array")
        }
        return root
    }

    private fun execute(request: Request): Any {
        val response = okHttpClient.newCall(request.withSameOriginHeaders(requireBaseUrl())).execute()
        response.use { res ->
            if (!res.isSuccessful) {
                throw IllegalStateException("HTTP ${res.code}: ${res.message}")
            }
            val bodyString = res.body?.string().orEmpty()
            if (bodyString.isBlank()) {
                return JSONObject()
            }
            return JSONTokener(bodyString).nextValue()
        }
    }

    private fun apiUrl(vararg segments: String): HttpUrl {
        val builder = requireBaseUrl().newBuilder()
            .encodedPath("/api/v3/")
            .query(null)
            .fragment(null)
        for (segment in segments) {
            builder.addPathSegment(segment)
        }
        return builder.build()
    }

    private fun requireBaseUrl(): HttpUrl =
        baseUrl.toHttpUrlOrNull()
            ?: throw IllegalStateException("Invalid xg2g server URL: $baseUrl")

    private companion object {
        private const val SESSION_COOKIE_NAME = "xg2g_session"
    }
}
