package io.github.manugh.xg2g.android.recordings

import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.apiV3Url
import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.auth.createNativeAuthenticatedOkHttpClient
import io.github.manugh.xg2g.android.guide.GuideAuthRequiredException
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
    ): RecordingsResponse = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext RecordingsResponse()
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
            ?: return@withContext RecordingsResponse()

        val reqId = json.optString("requestId").takeIf { it.isNotBlank() }
        val curRoot = json.optString("currentRoot").takeIf { it.isNotBlank() }
        val curPath = json.optString("currentPath").takeIf { it.isNotBlank() }

        val rootsList = mutableListOf<RecordingRoot>()
        val rootsArr = json.optJSONArray("roots")
        if (rootsArr != null) {
            for (i in 0 until rootsArr.length()) {
                val obj = rootsArr.optJSONObject(i) ?: continue
                val id = obj.optString("id").takeIf { it.isNotBlank() } ?: continue
                val name = obj.optString("name").takeIf { it.isNotBlank() } ?: id
                rootsList.add(RecordingRoot(id = id, name = name))
            }
        }

        val dirsList = mutableListOf<DirectoryItem>()
        val dirsArr = json.optJSONArray("directories")
        if (dirsArr != null) {
            for (i in 0 until dirsArr.length()) {
                val obj = dirsArr.optJSONObject(i) ?: continue
                val name = obj.optString("name").takeIf { it.isNotBlank() } ?: continue
                val dirPath = obj.optString("path").takeIf { it.isNotBlank() } ?: ""
                dirsList.add(DirectoryItem(name = name, path = dirPath))
            }
        }

        val crumbsList = mutableListOf<Breadcrumb>()
        val crumbsArr = json.optJSONArray("breadcrumbs")
        if (crumbsArr != null) {
            for (i in 0 until crumbsArr.length()) {
                val obj = crumbsArr.optJSONObject(i) ?: continue
                val name = obj.optString("name").takeIf { it.isNotBlank() } ?: continue
                val crumbPath = obj.optString("path").takeIf { it.isNotBlank() } ?: ""
                crumbsList.add(Breadcrumb(name = name, path = crumbPath))
            }
        }

        val itemsArr = json.optJSONArray("recordings") ?: JSONArray()
        val recordingItems = parseRecordingItems(itemsArr)

        RecordingsResponse(
            requestId = reqId,
            currentRoot = curRoot,
            currentPath = curPath,
            roots = rootsList,
            directories = dirsList,
            breadcrumbs = crumbsList,
            recordings = recordingItems
        )
    }

    suspend fun fetchContinueWatching(authToken: String?, limit: Int = 12): List<RecordingItem> = withContext(Dispatchers.IO) {
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

    private fun parseRecordingItems(arr: JSONArray): List<RecordingItem> {
        val list = mutableListOf<RecordingItem>()
        for (i in 0 until arr.length()) {
            val obj = arr.optJSONObject(i) ?: continue
            val recId = obj.optString("recordingId").takeIf { it.isNotBlank() }
                ?: obj.optString("id").takeIf { it.isNotBlank() }
                ?: continue

            val resumeObj = obj.optJSONObject("resume")
            val resume = resumeObj?.let { r ->
                ResumeSummary(
                    posSeconds = r.optLong("posSeconds", 0L),
                    durationSeconds = if (r.has("durationSeconds") && !r.isNull("durationSeconds")) r.optLong("durationSeconds") else null,
                    finished = if (r.has("finished") && !r.isNull("finished")) r.optBoolean("finished") else null,
                    updatedAt = r.optString("updatedAt").takeIf { it.isNotBlank() }
                )
            }

            val item = RecordingItem(
                recordingId = recId,
                serviceRef = obj.optString("serviceRef").takeIf { it.isNotBlank() },
                title = obj.optString("title").takeIf { it.isNotBlank() } ?: "Aufnahme",
                description = obj.optString("description").takeIf { it.isNotBlank() },
                beginUnixSeconds = if (obj.has("beginUnixSeconds") && !obj.isNull("beginUnixSeconds")) obj.optLong("beginUnixSeconds") else null,
                length = obj.optString("length").takeIf { it.isNotBlank() },
                durationSeconds = if (obj.has("durationSeconds") && !obj.isNull("durationSeconds")) obj.optLong("durationSeconds") else null,
                filename = obj.optString("filename").takeIf { it.isNotBlank() },
                status = obj.optString("status").takeIf { it.isNotBlank() },
                localWritable = if (obj.has("localWritable") && !obj.isNull("localWritable")) obj.optBoolean("localWritable") else null,
                resume = resume
            )
            list.add(item)
        }
        return list
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
