package io.github.manugh.xg2g.android.transport.recordings

import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.contract.Breadcrumb
import io.github.manugh.xg2g.android.contract.DirectoryItem
import io.github.manugh.xg2g.android.contract.RecordingItem
import io.github.manugh.xg2g.android.contract.RecordingItem as WireRecordingItem
import io.github.manugh.xg2g.android.contract.RecordingResponse as WireRecordingResponse
import io.github.manugh.xg2g.android.contract.RecordingRoot
import io.github.manugh.xg2g.android.contract.ResumeSummary
import io.github.manugh.xg2g.android.recordings.RecordingListItem
import io.github.manugh.xg2g.android.recordings.RecordingsPage
import io.github.manugh.xg2g.android.transport.apiV3Url
import io.github.manugh.xg2g.android.transport.auth.createNativeAuthenticatedOkHttpClient
import io.github.manugh.xg2g.android.transport.guide.GuideAuthRequiredException
import io.github.manugh.xg2g.android.transport.playback.withSameOriginHeaders
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



internal class RecordingsApiClient(
    private val baseUrlProvider: () -> String,
    stateStore: PersistedDeviceAuthStateStore,
    dpopProvider: DPoPProvider,
    stateMachine: AuthStateMachine? = null,
    private val okHttpClient: OkHttpClient = createNativeAuthenticatedOkHttpClient(
        stateStore = stateStore,
        dpopProvider = dpopProvider,
        stateMachine = stateMachine
    )
) {
    constructor(
        baseUrl: String,
        stateStore: PersistedDeviceAuthStateStore,
        dpopProvider: DPoPProvider,
        stateMachine: AuthStateMachine? = null,
        okHttpClient: OkHttpClient = createNativeAuthenticatedOkHttpClient(stateStore, dpopProvider, stateMachine)
    ) : this(
        baseUrlProvider = { baseUrl },
        stateStore = stateStore,
        dpopProvider = dpopProvider,
        stateMachine = stateMachine,
        okHttpClient = okHttpClient
    )

    private val baseUrl: String get() = baseUrlProvider()
    suspend fun fetchRecordings(
        authToken: String?,
        root: String? = null,
        path: String? = null
    ): RecordingsPage = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext RecordingsPage()
        }
        ensureAuthSession(authToken)
        val urlBuilder = apiUrl("recordings").newBuilder()
        if (!root.isNullOrBlank()) {
            urlBuilder.addQueryParameter("root", root.trim())
        }
        if (!path.isNullOrBlank()) {
            urlBuilder.addQueryParameter("path", path.trim())
        }
        val url = urlBuilder.build()
        val request = Request.Builder().url(url).get().build().withSameOriginHeaders(url)
        val response = okHttpClient.newCall(request).execute()

        val responseBody = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            when (response.code) {
                401 -> throw GuideAuthRequiredException(response.code, "Unauthorized")
                403 -> throw GuideAuthRequiredException(response.code, "Forbidden")
                else -> throw IllegalStateException("Recordings API request failed with HTTP ${response.code}")
            }
        }

        val json = JSONTokener(responseBody).nextValue() as? JSONObject
            ?: return@withContext RecordingsPage()

        // Decoding is the generated contract's job; this client only maps the
        // result into what the recordings screen works with.
        WireRecordingResponse.fromJson(json).toDomain()
    }

    suspend fun fetchContinueWatching(authToken: String?, limit: Int = 12): List<RecordingListItem> = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
        ensureAuthSession(authToken)
        val url = apiUrl("recordings", "continue").newBuilder()
            .addQueryParameter("limit", limit.toString())
            .build()

        val request = Request.Builder().url(url).get().build().withSameOriginHeaders(url)
        val response = okHttpClient.newCall(request).execute()

        val responseBody = response.body?.string().orEmpty()
        if (!response.isSuccessful) {
            return@withContext emptyList()
        }

        val json = JSONTokener(responseBody).nextValue() as? JSONObject
            ?: return@withContext emptyList()

        val itemsArr = json.optJSONArray("items") ?: json.optJSONArray("recordings") ?: JSONArray()
        parseRecordingItems(itemsArr)
    }

    fun buildThumbnailUrl(recordingId: String): String =
        recordingThumbnailUrl(requireBaseUrl(), recordingId).toString()

    private fun parseRecordingItems(arr: JSONArray): List<RecordingListItem> =
        (0 until arr.length()).mapNotNull { index ->
            val obj = arr.optJSONObject(index) ?: return@mapNotNull null
            runCatching { WireRecordingItem.fromJson(obj).toDomain() }.getOrNull()
        }

    private suspend fun ensureAuthSession(authToken: String?) {
        // Native REST API requests manage authentication per request
    }

    private fun apiUrl(vararg segments: String): HttpUrl =
        apiV3Url(requireBaseUrl(), *segments)

    private fun requireBaseUrl(): HttpUrl =
        baseUrl.toHttpUrlOrNull()
            ?: throw IllegalArgumentException("Invalid server base URL: $baseUrl")

    companion object {
        private const val SESSION_COOKIE_NAME = "xg2g_session"
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
