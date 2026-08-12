package io.github.manugh.xg2g.android.timers

import android.webkit.CookieManager
import io.github.manugh.xg2g.android.guide.GuideAuthRequiredException
import io.github.manugh.xg2g.android.playback.net.AuthCookieSession
import io.github.manugh.xg2g.android.playback.net.CookieBackedAuthSession
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
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
import org.json.JSONTokener

internal class TimersApiClient(
    private val baseUrlProvider: () -> String,
    private val cookieSession: AuthCookieSession = CookieBackedAuthSession(CookieManager.getInstance()),
    private val okHttpClient: OkHttpClient = OkHttpClient.Builder()
        .addInterceptor { chain ->
            val original = chain.request()
            val builder = original.newBuilder()
            cookieSession.applyCookies(original.url, builder)
            val response = chain.proceed(builder.build())
            cookieSession.storeCookies(original.url, response.headers)
            response
        }
        .build()
) {
    constructor(
        baseUrl: String,
        cookieSession: AuthCookieSession = CookieBackedAuthSession(CookieManager.getInstance()),
        okHttpClient: OkHttpClient = OkHttpClient.Builder()
            .addInterceptor { chain ->
                val original = chain.request()
                val builder = original.newBuilder()
                cookieSession.applyCookies(original.url, builder)
                val response = chain.proceed(builder.build())
                cookieSession.storeCookies(original.url, response.headers)
                response
            }
            .build()
    ) : this(
        baseUrlProvider = { baseUrl },
        cookieSession = cookieSession,
        okHttpClient = okHttpClient
    )

    private val baseUrl: String get() = baseUrlProvider()
    suspend fun fetchTimers(authToken: String?): List<TimerItem> = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
        ensureAuthSession(authToken)
        val url = apiUrl("timers")
        val requestBuilder = Request.Builder().url(url).get()
        val token = authToken?.trim()
        if (!token.isNullOrEmpty()) {
            requestBuilder.header("Authorization", "Bearer $token")
        }

        val request = requestBuilder.build().withSameOriginHeaders(url)
        val response = okHttpClient.newCall(request).execute()

        val responseBody = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            when (response.code) {
                401 -> throw GuideAuthRequiredException(response.code, "Unauthorized")
                403 -> throw GuideAuthRequiredException(response.code, "Forbidden")
                else -> throw IllegalStateException("Timers API request failed with HTTP ${response.code}")
            }
        }

        val parsed = JSONTokener(responseBody).nextValue()
        val itemsArr = when (parsed) {
            is JSONArray -> parsed
            is JSONObject -> parsed.optJSONArray("timers") ?: JSONArray()
            else -> JSONArray()
        }

        parseTimerItems(itemsArr)
    }

    private fun parseTimerItems(arr: JSONArray): List<TimerItem> {
        val list = mutableListOf<TimerItem>()
        for (i in 0 until arr.length()) {
            val obj = arr.optJSONObject(i) ?: continue
            val id = obj.optString("timerId")
                .takeIf { it.isNotBlank() } ?: obj.optString("id").takeIf { it.isNotBlank() } ?: continue

            val title = obj.optString("title").takeIf { it.isNotBlank() }
            val sRef = obj.optString("serviceRef").takeIf { it.isNotBlank() }
                ?: obj.optString("serviceref").takeIf { it.isNotBlank() }
            val sName = obj.optString("serviceName").takeIf { it.isNotBlank() }
                ?: obj.optString("servicename").takeIf { it.isNotBlank() }

            val begin = obj.optLong("beginUnixSeconds", obj.optLong("begin", 0L))
            val end = obj.optLong("endUnixSeconds", obj.optLong("end", 0L))
            val state = obj.optString("state").takeIf { it.isNotBlank() }
            val disabled = obj.optBoolean("disabled", false)
            val justPlay = obj.optBoolean("justPlay", obj.optBoolean("justplay", false))
            val description = obj.optString("description").takeIf { it.isNotBlank() }

            list.add(
                TimerItem(
                    timerId = id,
                    title = title,
                    serviceRef = sRef,
                    serviceName = sName,
                    beginUnixSeconds = begin,
                    endUnixSeconds = end,
                    state = state,
                    disabled = disabled,
                    justPlay = justPlay,
                    description = description
                )
            )
        }
        return list
    }

    private suspend fun ensureAuthSession(authToken: String?) = withContext(Dispatchers.IO) {
        val currentBaseUrl = baseUrl
        if (currentBaseUrl.isBlank()) {
            return@withContext
        }
        val sessionUrl = apiUrl("auth", "session")
        if (cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)) {
            return@withContext
        }

        val token = authToken?.trim()
        if (token.isNullOrEmpty()) {
            return@withContext
        }

        val request = Request.Builder()
            .url(sessionUrl)
            .post("{}".toRequestBody(JSON_MEDIA_TYPE))
            .header("Authorization", "Bearer $token")
            .build()
            .withSameOriginHeaders(sessionUrl)

        try {
            okHttpClient.newCall(request).execute().use { response ->
                if (response.isSuccessful) {
                    cookieSession.storeCookies(sessionUrl, response.headers)
                }
            }
        } catch (_: Exception) {
        }
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
        private const val SESSION_COOKIE_NAME = "xg2g_session"
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
