package io.github.manugh.xg2g.android.guide

import android.webkit.CookieManager
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

internal class GuideApiClient(
    private val baseUrl: String,
    private val cookieSession: CookieBackedAuthSession = CookieBackedAuthSession(CookieManager.getInstance()),
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
    suspend fun ensureAuthSession(authToken: String?) {
        withContext(Dispatchers.IO) {
            val sessionUrl = apiUrl("auth", "session")
            val bearerToken = authToken?.trim().takeIf { !it.isNullOrEmpty() }
            if (bearerToken == null && cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)) {
                return@withContext
            }

            val requestBuilder = Request.Builder()
                .url(sessionUrl)
                .post(ByteArray(0).toRequestBody(null))

            if (bearerToken != null) {
                requestBuilder.header("Authorization", "Bearer $bearerToken")
            }

            execute(requestBuilder.build()).use { response ->
                if (!response.isSuccessful) {
                    throw mapHttpException(response.code, response.message, response.body.string())
                }
            }
        }
    }

    suspend fun fetchBouquets(authToken: String?): List<GuideBouquet> = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val request = Request.Builder()
            .url(apiUrl("services", "bouquets"))
            .get()
            .build()

        executeJsonArray(request).mapNotNull { item ->
            val name = item.optString("name").trim()
            if (name.isEmpty()) {
                null
            } else {
                GuideBouquet(
                    name = name,
                    services = item.optInt("services", 0)
                )
            }
        }
    }

    suspend fun fetchChannels(
        authToken: String?,
        bouquetName: String?
    ): List<GuideChannel> = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val urlBuilder = apiUrlBuilder("services")
        bouquetName?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { urlBuilder.addQueryParameter("bouquet", it) }
        val request = Request.Builder()
            .url(urlBuilder.build())
            .get()
            .build()

        executeJsonArray(request).mapNotNull { item ->
            val serviceRef = item.optString("serviceRef")
                .ifBlank { item.optString("id") }
                .trim()
            if (serviceRef.isEmpty()) {
                null
            } else {
                GuideChannel(
                    serviceRef = serviceRef,
                    name = item.optString("name").ifBlank { serviceRef },
                    number = item.optString("number").takeIf { it.isNotBlank() },
                    group = item.optString("group").takeIf { it.isNotBlank() },
                    logoUrl = item.optString("logoUrl").takeIf { it.isNotBlank() },
                    resolution = item.optString("resolution").takeIf { it.isNotBlank() },
                    codec = item.optString("codec").takeIf { it.isNotBlank() }
                )
            }
        }
    }

    suspend fun fetchNowNext(
        authToken: String?,
        serviceRefs: List<String>
    ): Map<String, Pair<GuideProgram?, GuideProgram?>> = withContext(Dispatchers.IO) {
        if (serviceRefs.isEmpty()) {
            return@withContext emptyMap()
        }

        ensureAuthSession(authToken)
        val services = JSONArray()
        serviceRefs.forEach(services::put)
        val request = Request.Builder()
            .url(apiUrl("services", "now-next"))
            .post(
                JSONObject()
                    .put("services", services)
                    .toString()
                    .toRequestBody(JSON_MEDIA_TYPE)
            )
            .build()

        val root = executeJsonObject(request)
        val items = root.optJSONArray("items") ?: JSONArray()
        buildMap {
            for (index in 0 until items.length()) {
                val item = items.optJSONObject(index) ?: continue
                val serviceRef = item.optString("serviceRef").trim()
                if (serviceRef.isEmpty()) {
                    continue
                }
                put(
                    serviceRef,
                    parseProgram(item.optJSONObject("now")) to parseProgram(item.optJSONObject("next"))
                )
            }
        }
    }

    private fun execute(request: Request) =
        okHttpClient.newCall(request.withSameOriginHeaders(requireBaseUrl())).execute()

    private fun executeJsonArray(request: Request): List<JSONObject> {
        execute(request).use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw mapHttpException(response.code, response.message, body)
            }
            val array = JSONArray(body.ifBlank { "[]" })
            return buildList {
                for (index in 0 until array.length()) {
                    array.optJSONObject(index)?.let(::add)
                }
            }
        }
    }

    private fun executeJsonObject(request: Request): JSONObject {
        execute(request).use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw mapHttpException(response.code, response.message, body)
            }
            return if (body.isBlank()) JSONObject() else JSONObject(body)
        }
    }

    private fun parseProgram(item: JSONObject?): GuideProgram? {
        if (item == null) {
            return null
        }

        val title = item.optString("title").trim()
        val start = item.optLong("start")
        val end = item.optLong("end")
        if (title.isEmpty() || start <= 0L || end <= 0L) {
            return null
        }

        return GuideProgram(
            title = title,
            startEpochSec = start,
            endEpochSec = end
        )
    }

    private fun apiUrl(vararg segments: String): HttpUrl = apiUrlBuilder(*segments).build()

    private fun apiUrlBuilder(vararg segments: String): HttpUrl.Builder =
        requireBaseUrl().newBuilder()
            .encodedPath("/api/v3/")
            .query(null)
            .fragment(null)
            .apply {
                segments.forEach(::addPathSegment)
            }

    private fun requireBaseUrl(): HttpUrl =
        baseUrl.toHttpUrlOrNull()
            ?: throw IllegalStateException("Invalid xg2g server URL: $baseUrl")

    private fun mapHttpException(code: Int, message: String, body: String?): Throwable {
        if (code == 401 || code == 403) {
            return GuideAuthRequiredException(code)
        }
        val detail = body?.trim().takeIf { !it.isNullOrEmpty() }?.let { " · $it" }.orEmpty()
        return IllegalStateException("Guide API $code: $message$detail")
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        const val SESSION_COOKIE_NAME = "xg2g_session"
    }
}

internal class GuideAuthRequiredException(
    val statusCode: Int
) : IllegalStateException("Guide auth required ($statusCode)")
