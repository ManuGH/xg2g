package io.github.manugh.xg2g.android.guide

import android.webkit.CookieManager
import io.github.manugh.xg2g.android.DeviceAuthReenrollRequiredException
import io.github.manugh.xg2g.android.DeviceAuthRepository
import io.github.manugh.xg2g.android.DeviceAuthSignInRequiredException
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
import java.time.OffsetDateTime

internal class GuideApiClient(
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
    suspend fun ensureAuthSession(authToken: String?) {
        withContext(Dispatchers.IO) {
            val sessionUrl = apiUrl("auth", "session")
            val repository = deviceAuthRepository
            if (repository != null) {
                try {
                    repository.ensureAuthSession(baseUrl, authToken)
                    return@withContext
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

            execute(request).use { response ->
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

    suspend fun fetchEpgWindow(
        authToken: String?,
        bouquetName: String?,
        timelineWindow: GuideTimelineWindow
    ): Map<String, List<GuideProgram>> = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val urlBuilder = apiUrlBuilder("epg")
            .addQueryParameter("from", timelineWindow.startEpochSec.toString())
            .addQueryParameter("to", timelineWindow.endEpochSec.toString())
        bouquetName?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { urlBuilder.addQueryParameter("bouquet", it) }
        val request = Request.Builder()
            .url(urlBuilder.build())
            .get()
            .build()

        val byServiceRef = linkedMapOf<String, MutableList<GuideProgram>>()
        executeJsonArray(request).forEach { item ->
            val serviceRef = canonicalGuideServiceRef(item.optString("serviceRef"))
            if (serviceRef.isEmpty()) {
                return@forEach
            }
            parseProgram(
                item = item,
                titleKey = "title",
                startKey = "start",
                endKey = "end",
                descriptionKey = "desc"
            )?.let { program ->
                byServiceRef.getOrPut(serviceRef) { mutableListOf() }.add(program)
            }
        }

        buildMap {
            byServiceRef.forEach { (serviceRef, programs) ->
                if (serviceRef.isEmpty()) {
                    return@forEach
                }
                put(serviceRef, programs.sortedBy(GuideProgram::startEpochSec))
            }
        }
    }

    suspend fun fetchHealthStatus(authToken: String?): GuideHealthStatus = withContext(Dispatchers.IO) {
        ensureAuthSession(authToken)
        val request = Request.Builder()
            .url(apiUrl("system", "health"))
            .get()
            .build()

        val root = executeJsonObject(request)
        val receiverStatus = root.optJSONObject("receiver")
            ?.optString("status")
            ?.trim()
            ?.lowercase()
            .orEmpty()
        val epgNode = root.optJSONObject("epg")
        val epgStatus = epgNode
            ?.optString("status")
            ?.trim()
            ?.lowercase()
            .orEmpty()
        val serverTime = root.optString("serverTime")
            .trim()
            .takeIf { it.isNotEmpty() }
            ?.let { raw ->
                runCatching { OffsetDateTime.parse(raw) }.getOrNull()
            }

        GuideHealthStatus(
            receiverHealthy = receiverStatus == "ok",
            epgHealthy = epgStatus == "ok",
            missingChannels = epgNode
                ?.takeIf { it.has("missingChannels") }
                ?.optInt("missingChannels"),
            serverTimeEpochSec = serverTime?.toEpochSecond(),
            serverTimeOffsetSeconds = serverTime?.offset?.totalSeconds
        )
    }

    private fun execute(request: Request) =
        okHttpClient.newCall(request.withSameOriginHeaders(requireBaseUrl())).execute().also { response ->
            if (response.code == 401 || response.code == 403) {
                clearSessionCookie()
            }
        }

    private fun executeJsonArray(request: Request): List<JSONObject> {
        execute(request).use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw mapHttpException(response.code, response.message, body)
            }
            val array = decodeJsonArray(body, request.url.encodedPath)
            return buildList {
                for (index in 0 until array.length()) {
                    array.optJSONObject(index)?.let(::add)
                }
            }
        }
    }

    private fun decodeJsonArray(body: String, path: String): JSONArray {
        val raw = body.trim()
        if (raw.isEmpty()) {
            return JSONArray()
        }

        return when (val parsed = JSONTokener(raw).nextValue()) {
            JSONObject.NULL -> JSONArray()
            is JSONArray -> parsed
            is JSONObject -> parsed.optJSONArray("items")
                ?: throw IllegalStateException("Guide API expected array response for $path")
            else -> throw IllegalStateException("Guide API expected array response for $path")
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

    private fun parseProgram(
        item: JSONObject?,
        titleKey: String = "title",
        startKey: String = "start",
        endKey: String = "end",
        descriptionKey: String? = null
    ): GuideProgram? {
        if (item == null) {
            return null
        }

        val title = item.optString(titleKey).trim()
        val start = item.optLong(startKey)
        val end = item.optLong(endKey)
        if (title.isEmpty() || start <= 0L || end <= 0L) {
            return null
        }

        return GuideProgram(
            title = title,
            startEpochSec = start,
            endEpochSec = end,
            description = descriptionKey?.let(item::optString)?.trim()?.takeIf { it.isNotEmpty() },
            startXmltv = item.optString("startXmltv").trim().takeIf { it.isNotEmpty() },
            endXmltv = item.optString("endXmltv").trim().takeIf { it.isNotEmpty() }
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
        val problemDetail = extractProblemDetail(body)
        if (code == 401 || code == 403) {
            return GuideAuthRequiredException(code, problemDetail)
        }
        val detail = problemDetail?.let { " · $it" }.orEmpty()
        return IllegalStateException("Guide API $code: $message$detail")
    }

    private fun clearSessionCookie() {
        cookieSession.clearSessionCookie(
            url = apiUrl("auth", "session"),
            cookieName = SESSION_COOKIE_NAME
        )
    }

    private fun extractProblemDetail(body: String?): String? {
        val raw = body?.trim()?.takeIf { it.isNotEmpty() } ?: return null
        return runCatching {
            JSONObject(raw).optString("detail").takeIf { it.isNotBlank() }
        }.getOrNull() ?: raw
    }

    private companion object {
        const val SESSION_COOKIE_NAME = "xg2g_session"
    }
}

internal class GuideAuthRequiredException(
    val statusCode: Int,
    detail: String? = null
) : IllegalStateException(detail?.takeIf { it.isNotBlank() } ?: "Guide auth required ($statusCode)")
