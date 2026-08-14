package io.github.manugh.xg2g.android.dashboard

import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.auth.createNativeAuthenticatedOkHttpClient
import io.github.manugh.xg2g.android.guide.GuideAuthRequiredException
import io.github.manugh.xg2g.android.guide.GuideHealthStatus
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import org.json.JSONTokener

internal data class HouseholdUnlockStatus(
    val pinConfigured: Boolean,
    val unlocked: Boolean
)

internal data class SystemScanStatus(
    val state: String = "idle",
    val startedAt: Long? = null,
    val finishedAt: Long? = null,
    val totalChannels: Int = 0,
    val scannedChannels: Int = 0,
    val updatedCount: Int = 0,
    val lastError: String? = null
)

internal data class NativeHouseholdProfile(
    val id: String,
    val name: String,
    val kind: String,
    val maxFsk: Int? = null,
    val allowedBouquets: List<String> = emptyList(),
    val allowedServiceRefs: List<String> = emptyList(),
    val favoriteServiceRefs: List<String> = emptyList()
)

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
    private val baseUrlProvider: () -> String,
    private val profileIdProvider: () -> String? = { null },
    stateStore: PersistedDeviceAuthStateStore? = null,
    dpopProvider: DPoPProvider? = null,
    private val okHttpClient: OkHttpClient = if (stateStore != null && dpopProvider != null) {
        createNativeAuthenticatedOkHttpClient(stateStore, dpopProvider, profileIdProvider = profileIdProvider)
    } else {
        OkHttpClient()
    }
) {
    constructor(
        baseUrl: String,
        profileIdProvider: () -> String? = { null },
        stateStore: PersistedDeviceAuthStateStore? = null,
        dpopProvider: DPoPProvider? = null
    ) : this(
        baseUrlProvider = { baseUrl },
        profileIdProvider = profileIdProvider,
        stateStore = stateStore,
        dpopProvider = dpopProvider
    )

    private val baseUrl: String get() = baseUrlProvider()
    private suspend fun ensureAuthSession(authToken: String?) {
        // Native REST API requests manage authentication per request
    }

    suspend fun fetchHealth(authToken: String?): GuideHealthStatus = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext GuideHealthStatus(receiverHealthy = false, epgHealthy = false, missingChannels = 0)
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("system", "health")).get()
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
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("recordings")).get()
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
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("timers")).get()
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
        if (baseUrl.isBlank()) {
            return@withContext DashboardDvrStatus(diskFreeBytes = null, diskTotalBytes = null, recordingCount = 0, activeTimerCount = 0)
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("dvr", "status")).get()
        val root = executeJsonObject(requestBuilder.build())
        DashboardDvrStatus(
            diskFreeBytes = root.optLong("diskFreeBytes").takeIf { it > 0 },
            diskTotalBytes = root.optLong("diskTotalBytes").takeIf { it > 0 },
            recordingCount = root.optInt("recordingCount", 0),
            activeTimerCount = root.optInt("activeTimerCount", 0)
        )
    }

    suspend fun fetchHouseholdUnlockStatus(authToken: String?): HouseholdUnlockStatus = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext HouseholdUnlockStatus(pinConfigured = false, unlocked = false)
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("household", "unlock")).get()
        val root = executeJsonObject(requestBuilder.build())
        HouseholdUnlockStatus(
            pinConfigured = root.optBoolean("pinConfigured", false),
            unlocked = root.optBoolean("unlocked", false)
        )
    }

    suspend fun fetchHouseholdProfiles(authToken: String?): List<NativeHouseholdProfile> = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext emptyList()
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("household", "profiles")).get()
        val root = execute(requestBuilder.build())
        val array = when (root) {
            is JSONArray -> root
            is JSONObject -> root.optJSONArray("profiles") ?: JSONArray()
            else -> JSONArray()
        }
        val items = mutableListOf<NativeHouseholdProfile>()
        for (i in 0 until array.length()) {
            val obj = array.optJSONObject(i) ?: continue
            val id = obj.optString("id")
            val name = obj.optString("name")
            if (id.isNotBlank() && name.isNotBlank()) {
                val kind = obj.optString("kind", "adult")
                val maxFsk = if (obj.has("maxFsk") && !obj.isNull("maxFsk")) obj.optInt("maxFsk") else null

                val bouquets = mutableListOf<String>()
                obj.optJSONArray("allowedBouquets")?.let { bArr ->
                    for (b in 0 until bArr.length()) { bArr.optString(b).takeIf { it.isNotBlank() }?.let { bouquets.add(it) } }
                }

                val services = mutableListOf<String>()
                obj.optJSONArray("allowedServiceRefs")?.let { sArr ->
                    for (s in 0 until sArr.length()) { sArr.optString(s).takeIf { it.isNotBlank() }?.let { services.add(it) } }
                }

                val favorites = mutableListOf<String>()
                obj.optJSONArray("favoriteServiceRefs")?.let { fArr ->
                    for (f in 0 until fArr.length()) { fArr.optString(f).takeIf { it.isNotBlank() }?.let { favorites.add(it) } }
                }

                items.add(
                    NativeHouseholdProfile(
                        id = id,
                        name = name,
                        kind = kind,
                        maxFsk = maxFsk,
                        allowedBouquets = bouquets,
                        allowedServiceRefs = services,
                        favoriteServiceRefs = favorites
                    )
                )
            }
        }
        items
    }

    suspend fun unlockHousehold(authToken: String?, pin: String): HouseholdUnlockStatus = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext HouseholdUnlockStatus(pinConfigured = false, unlocked = false)
        }
        ensureAuthSession(authToken)
        val json = JSONObject().put("pin", pin)
        val requestBuilder = Request.Builder()
            .url(apiUrl("household", "unlock"))
            .post(json.toString().toRequestBody("application/json; charset=utf-8".toMediaType()))
        val root = executeJsonObject(requestBuilder.build())
        HouseholdUnlockStatus(
            pinConfigured = root.optBoolean("pinConfigured", false),
            unlocked = root.optBoolean("unlocked", false)
        )
    }

    suspend fun lockHousehold(authToken: String?): Unit = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("household", "unlock")).delete()
        execute(requestBuilder.build())
    }

    suspend fun fetchScanStatus(authToken: String?): SystemScanStatus = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext SystemScanStatus(
                state = "idle",
                startedAt = null,
                finishedAt = null,
                totalChannels = 0,
                scannedChannels = 0,
                updatedCount = 0,
                lastError = null
            )
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder().url(apiUrl("system", "scan")).get()
        val root = executeJsonObject(requestBuilder.build())
        SystemScanStatus(
            state = root.optString("state", "idle"),
            startedAt = root.optLong("startedAt").takeIf { it > 0 },
            finishedAt = root.optLong("finishedAt").takeIf { it > 0 },
            totalChannels = root.optInt("totalChannels", 0),
            scannedChannels = root.optInt("scannedChannels", 0),
            updatedCount = root.optInt("updatedCount", 0),
            lastError = root.optString("lastError").takeIf { it.isNotBlank() }
        )
    }

    suspend fun triggerSystemScan(authToken: String?): Boolean = withContext(Dispatchers.IO) {
        if (baseUrl.isBlank()) {
            return@withContext false
        }
        ensureAuthSession(authToken)
        val requestBuilder = Request.Builder()
            .url(apiUrl("system", "scan"))
            .post(ByteArray(0).toRequestBody(null))
        execute(requestBuilder.build())
        true
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

    private fun requireBaseUrl(): HttpUrl =
        baseUrl.toHttpUrlOrNull()
            ?: throw IllegalStateException("Invalid xg2g server URL: $baseUrl")

    private companion object {
        private const val SESSION_COOKIE_NAME = "xg2g_session"
    }
}
