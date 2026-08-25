package io.github.manugh.xg2g.android.transport.guide

import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.guide.GuideBouquet
import io.github.manugh.xg2g.android.guide.GuideChannel
import io.github.manugh.xg2g.android.guide.GuideHealthStatus
import io.github.manugh.xg2g.android.guide.GuideProgram
import io.github.manugh.xg2g.android.guide.GuideTimelineWindow
import io.github.manugh.xg2g.android.guide.canonicalGuideServiceRef
import io.github.manugh.xg2g.android.transport.apiV3UrlBuilder
import io.github.manugh.xg2g.android.transport.auth.createNativeAuthenticatedOkHttpClient
import io.github.manugh.xg2g.android.transport.playback.withSameOriginHeaders
import java.time.OffsetDateTime
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



internal class GuideApiClient(
    private val baseUrlProvider: () -> String,
    stateStore: PersistedDeviceAuthStateStore,
    dpopProvider: DPoPProvider,
    stateMachine: AuthStateMachine? = null,
    private val profileIdProvider: () -> String? = { null },
    private val okHttpClient: OkHttpClient = createNativeAuthenticatedOkHttpClient(
        stateStore = stateStore,
        dpopProvider = dpopProvider,
        stateMachine = stateMachine,
        profileIdProvider = profileIdProvider
    )
) {
    constructor(
        baseUrl: String,
        stateStore: PersistedDeviceAuthStateStore,
        dpopProvider: DPoPProvider,
        stateMachine: AuthStateMachine? = null,
        profileIdProvider: () -> String? = { null },
        okHttpClient: OkHttpClient = createNativeAuthenticatedOkHttpClient(stateStore, dpopProvider, stateMachine, profileIdProvider)
    ) : this(
        baseUrlProvider = { baseUrl },
        stateStore = stateStore,
        dpopProvider = dpopProvider,
        stateMachine = stateMachine,
        profileIdProvider = profileIdProvider,
        okHttpClient = okHttpClient
    )

    private val baseUrl: String get() = baseUrlProvider()
    suspend fun ensureAuthSession(authToken: String?) {
        // Native REST API requests manage authentication per request
    }

    suspend fun fetchBouquets(authToken: String?): List<GuideBouquet> = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
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
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
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
        if (baseUrl.isBlank()) {
            return@withContext emptyMap()
        }
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
        if (baseUrl.isBlank()) {
            return@withContext GuideHealthStatus(receiverHealthy = false, epgHealthy = false)
        }
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
        okHttpClient.newCall(request.withSameOriginHeaders(requireBaseUrl())).execute()

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
        apiV3UrlBuilder(requireBaseUrl(), *segments)

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
