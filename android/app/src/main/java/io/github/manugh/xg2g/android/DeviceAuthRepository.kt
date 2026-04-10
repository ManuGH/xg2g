package io.github.manugh.xg2g.android

import android.content.Context
import android.util.Log
import io.github.manugh.xg2g.android.playback.net.AuthCookieSession
import io.github.manugh.xg2g.android.playback.net.CookieBackedAuthSession
import io.github.manugh.xg2g.android.playback.net.resolveAgainst
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.Headers
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

internal class DeviceAuthRepository(
    private val stateStore: PersistedDeviceAuthStateStore,
    private val cookieSession: AuthCookieSession,
    private val transport: DeviceAuthTransport,
    private val telemetry: DeviceAuthTelemetry = LogcatDeviceAuthTelemetry(),
    private val nowEpochMs: () -> Long = { System.currentTimeMillis() }
) {
    @Volatile
    private var preparedSessionCookieBaseUrl: String? = null

    constructor(
        context: Context,
        cookieSession: AuthCookieSession = CookieBackedAuthSession(),
        stateStore: PersistedDeviceAuthStateStore = DeviceAuthStore(context.applicationContext),
        transport: DeviceAuthTransport = OkHttpDeviceAuthTransport(cookieSession)
    ) : this(
        stateStore = stateStore,
        cookieSession = cookieSession,
        transport = transport
    )

    fun applyLaunchCredentials(baseUrl: String, credentials: DeviceAuthLaunchCredentials?) {
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl) ?: return
        if (credentials == null) {
            return
        }
        preparedSessionCookieBaseUrl = null

        val current = currentState(normalizedBaseUrl)
        when {
            credentials.hasPersistableGrant() -> {
                stateStore.save(
                    PersistedDeviceAuthState(
                        serverUrl = normalizedBaseUrl,
                        deviceGrantId = credentials.deviceGrantId!!.trim(),
                        deviceGrant = credentials.deviceGrant!!.trim(),
                        accessSessionId = current?.accessSessionId,
                        accessToken = credentials.accessToken?.trim()?.takeIf { it.isNotEmpty() },
                        accessTokenExpiresAtEpochMs = credentials.accessTokenExpiresAtEpochMs,
                        policyVersion = current?.policyVersion,
                        publishedEndpoints = current?.publishedEndpoints.orEmpty()
                    )
                )
            }

            current != null && !credentials.accessToken.isNullOrBlank() -> {
                stateStore.save(
                    current.copy(
                        accessToken = credentials.accessToken.trim(),
                        accessTokenExpiresAtEpochMs = credentials.accessTokenExpiresAtEpochMs
                    )
                )
            }
        }
    }

    fun clearPersistedState() {
        preparedSessionCookieBaseUrl = null
        stateStore.clear()
    }

    suspend fun ensureAuthSession(baseUrl: String, legacyAuthToken: String?) {
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl)
            ?: throw IllegalStateException("Invalid xg2g server URL: $baseUrl")
        val uiBaseUrl = normalizedBaseUrl.toHttpUrlOrNull()
            ?: throw IllegalStateException("Invalid xg2g server URL: $normalizedBaseUrl")
        val sessionUrl = apiUrl(uiBaseUrl, "auth", "session")
        val deviceState = currentState(normalizedBaseUrl)
        val hasSessionCookie = cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)
        if (deviceState == null && hasSessionCookie) {
            return
        }
        if (deviceState != null && hasSessionCookie && preparedSessionCookieBaseUrl == normalizedBaseUrl) {
            return
        }

        if (deviceState != null) {
            val bearer = resolveDeviceAccessToken(uiBaseUrl, forceRefresh = false)
            try {
                transport.createCookieSession(uiBaseUrl, bearer)
                preparedSessionCookieBaseUrl = normalizedBaseUrl
                return
            } catch (error: DeviceAuthHttpException) {
                if (error.statusCode in setOf(401, 403, 410)) {
                    clearAccessSessionArtifacts(normalizedBaseUrl)
                    val refreshedBearer = resolveDeviceAccessToken(uiBaseUrl, forceRefresh = true)
                    try {
                        transport.createCookieSession(uiBaseUrl, refreshedBearer)
                        preparedSessionCookieBaseUrl = normalizedBaseUrl
                        return
                    } catch (retryError: DeviceAuthHttpException) {
                        if (retryError.statusCode in setOf(401, 403, 404, 410)) {
                            requireReenroll(
                                baseUrl = normalizedBaseUrl,
                                stage = "cookie_session_refresh_retry",
                                error = retryError,
                                message = "Android device pairing is no longer valid. Pair this device again."
                            )
                        }
                        throw unavailable(
                            stage = "cookie_session_refresh_retry",
                            message = "Android could not refresh its browser session.",
                            error = retryError
                        )
                    }
                }
                throw unavailable(
                    stage = "cookie_session_refresh",
                    message = "Android could not refresh its browser session.",
                    error = error
                )
            }
        }

        val legacyBearer = legacyAuthToken?.trim().takeIf { !it.isNullOrEmpty() } ?: return
        telemetry.record(
            DeviceAuthTelemetryEvent(
                name = "legacy_auth_token_fallback",
                level = DeviceAuthTelemetryLevel.INFO,
                stage = "ensure_auth_session",
                outcome = "used"
            )
        )
        try {
            transport.createCookieSession(uiBaseUrl, legacyBearer)
            preparedSessionCookieBaseUrl = normalizedBaseUrl
        } catch (error: DeviceAuthHttpException) {
            if (error.statusCode in setOf(401, 403, 410)) {
                requireSignIn(
                    baseUrl = normalizedBaseUrl,
                    stage = "legacy_cookie_session_exchange",
                    error = error,
                    message = "Android sign-in is required. Open xg2g from the web tools again."
                )
            }
            throw unavailable(
                stage = "legacy_cookie_session_exchange",
                message = "Android could not refresh its browser session.",
                error = error
            )
        }
    }

    suspend fun prepareWebSession(baseUrl: String, targetUrl: String, legacyAuthToken: String?): String {
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl)
            ?: throw IllegalStateException("Invalid xg2g server URL: $baseUrl")
        val uiBaseUrl = normalizedBaseUrl.toHttpUrlOrNull()
            ?: throw IllegalStateException("Invalid xg2g server URL: $normalizedBaseUrl")
        val targetPath = resolveTargetPath(uiBaseUrl, targetUrl)
        val sessionUrl = apiUrl(uiBaseUrl, "auth", "session")
        val deviceState = currentState(normalizedBaseUrl)

        if (deviceState != null) {
            telemetry.record(
                DeviceAuthTelemetryEvent(
                    name = "device_auth_web_session_prepare",
                    level = DeviceAuthTelemetryLevel.INFO,
                    stage = "prepare_web_session",
                    outcome = "device_bootstrap_required"
                )
            )
            return bootstrapDeviceWebSession(normalizedBaseUrl, uiBaseUrl, targetPath)
        }

        if (cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)) {
            telemetry.record(
                DeviceAuthTelemetryEvent(
                    name = "device_auth_web_session_prepare",
                    level = DeviceAuthTelemetryLevel.INFO,
                    stage = "prepare_web_session",
                    outcome = "reuse_cookie_session"
                )
            )
            return uiBaseUrl.resolveAgainst(targetPath)
        }

        ensureAuthSession(normalizedBaseUrl, legacyAuthToken)
        telemetry.record(
            DeviceAuthTelemetryEvent(
                name = "device_auth_web_session_prepare",
                level = DeviceAuthTelemetryLevel.INFO,
                stage = "prepare_web_session",
                outcome = "legacy_session_exchange"
            )
        )
        return uiBaseUrl.resolveAgainst(targetPath)
    }

    private suspend fun bootstrapDeviceWebSession(
        normalizedBaseUrl: String,
        uiBaseUrl: HttpUrl,
        targetPath: String
    ): String {
        telemetry.record(
            DeviceAuthTelemetryEvent(
                name = "device_auth_web_bootstrap_begin",
                level = DeviceAuthTelemetryLevel.INFO,
                stage = "web_bootstrap",
                outcome = "start"
            )
        )
        repeat(2) { attempt ->
            val started = startWebBootstrap(normalizedBaseUrl, uiBaseUrl, targetPath)
            try {
                val completed = transport.completeWebBootstrap(uiBaseUrl, started.completePath, started.bootstrapToken)
                telemetry.record(
                    DeviceAuthTelemetryEvent(
                        name = "device_auth_web_bootstrap_complete",
                        level = DeviceAuthTelemetryLevel.INFO,
                        stage = "web_bootstrap",
                        outcome = "session_cookie_ready"
                    )
                )
                return resolveBootstrapLocation(uiBaseUrl, completed.locationPath, targetPath)
            } catch (error: DeviceAuthHttpException) {
                if (attempt == 0 && error.statusCode in setOf(401, 403, 409, 410)) {
                    clearAccessSessionArtifacts(normalizedBaseUrl)
                    return@repeat
                }
                if (error.statusCode in setOf(401, 403, 404, 409, 410)) {
                    requireReenroll(
                        baseUrl = normalizedBaseUrl,
                        stage = "web_bootstrap_complete",
                        error = error,
                        message = "Android device pairing is no longer valid. Pair this device again."
                    )
                }
                throw unavailable(
                    stage = "web_bootstrap_complete",
                    message = "Android could not open the embedded xg2g session.",
                    error = error
                )
            }
        }

        throw unavailable(
            stage = "web_bootstrap_complete",
            message = "Android could not open the embedded xg2g session."
        )
    }

    private suspend fun startWebBootstrap(
        normalizedBaseUrl: String,
        uiBaseUrl: HttpUrl,
        targetPath: String
    ): StartedWebBootstrap {
        repeat(2) { attempt ->
            val accessToken = resolveDeviceAccessToken(
                uiBaseUrl = uiBaseUrl,
                forceRefresh = attempt > 0
            )
            try {
                val started = transport.startWebBootstrap(uiBaseUrl, accessToken, targetPath)
                telemetry.record(
                    DeviceAuthTelemetryEvent(
                        name = "device_auth_web_bootstrap_started",
                        level = DeviceAuthTelemetryLevel.INFO,
                        stage = "web_bootstrap_start",
                        outcome = "bootstrap_created"
                    )
                )
                return started
            } catch (error: DeviceAuthHttpException) {
                if (error.statusCode in setOf(401, 403, 410)) {
                    clearAccessSessionArtifacts(normalizedBaseUrl)
                    if (attempt == 0) {
                        return@repeat
                    }
                    requireReenroll(
                        baseUrl = normalizedBaseUrl,
                        stage = "web_bootstrap_start",
                        error = error,
                        message = "Android device pairing is no longer valid. Pair this device again."
                    )
                }
                throw unavailable(
                    stage = "web_bootstrap_start",
                    message = "Android could not start the embedded xg2g session.",
                    error = error
                )
            }
        }

        throw unavailable(
            stage = "web_bootstrap_start",
            message = "Android could not start the embedded xg2g session."
        )
    }

    private suspend fun resolveDeviceAccessToken(
        uiBaseUrl: HttpUrl,
        forceRefresh: Boolean
    ): String {
        var state = currentState(uiBaseUrl.toString())
            ?: throw DeviceAuthReenrollRequiredException(
                "Android device pairing is no longer valid. Pair this device again."
            )

        if (!forceRefresh && state.hasUsableAccessToken(nowEpochMs())) {
            telemetry.record(
                DeviceAuthTelemetryEvent(
                    name = "device_auth_access_token_ready",
                    level = DeviceAuthTelemetryLevel.INFO,
                    stage = "device_session_refresh",
                    outcome = "cached_access_token"
                )
            )
            return state.accessToken!!
        }

        repeat(2) { attempt ->
            try {
                val refreshed = transport.refreshSession(
                    uiBaseUrl = uiBaseUrl,
                    deviceGrantId = state.deviceGrantId,
                    deviceGrant = state.deviceGrant
                )
                val mergedEndpoints = mergePublishedEndpoints(
                    current = state.publishedEndpoints,
                    refreshed = refreshed.endpoints
                )
                val nextState = state.copy(
                    serverUrl = preferredNativeServerUrl(
                        currentServerUrl = uiBaseUrl.toString(),
                        endpoints = mergedEndpoints
                    ) ?: uiBaseUrl.toString(),
                    deviceGrantId = refreshed.rotatedDeviceGrantId ?: state.deviceGrantId,
                    deviceGrant = refreshed.rotatedDeviceGrant ?: state.deviceGrant,
                    accessSessionId = refreshed.accessSessionId,
                    accessToken = refreshed.accessToken,
                    accessTokenExpiresAtEpochMs = refreshed.accessTokenExpiresAtEpochMs,
                    policyVersion = refreshed.policyVersion,
                    publishedEndpoints = mergedEndpoints
                )
                stateStore.save(nextState)
                telemetry.record(
                    DeviceAuthTelemetryEvent(
                        name = "device_auth_access_token_ready",
                        level = DeviceAuthTelemetryLevel.INFO,
                        stage = "device_session_refresh",
                        outcome = if (refreshed.rotatedDeviceGrantId != null) {
                            "refreshed_and_rotated_grant"
                        } else {
                            "refreshed_access_token"
                        }
                    )
                )
                return refreshed.accessToken
            } catch (error: DeviceAuthHttpException) {
                val latest = currentState(uiBaseUrl.toString())
                if (attempt == 0 && latest != null) {
                    if (latest.hasUsableAccessToken(nowEpochMs())) {
                        telemetry.record(
                            DeviceAuthTelemetryEvent(
                                name = "device_auth_access_token_ready",
                                level = DeviceAuthTelemetryLevel.INFO,
                                stage = "device_session_refresh",
                                outcome = "concurrent_cached_access_token"
                            )
                        )
                        return latest.accessToken!!
                    }
                    if (latest.deviceGrantId != state.deviceGrantId || latest.deviceGrant != state.deviceGrant) {
                        state = latest
                        return@repeat
                    }
                }
                if (error.statusCode in setOf(401, 403, 404, 410)) {
                    requireReenroll(
                        baseUrl = uiBaseUrl.toString(),
                        stage = "device_session_refresh",
                        error = error,
                        message = "Android device pairing is no longer valid. Pair this device again."
                    )
                }
                throw unavailable(
                    stage = "device_session_refresh",
                    message = "Android could not refresh its device session.",
                    error = error
                )
            }
        }

        throw unavailable(
            stage = "device_session_refresh",
            message = "Android could not refresh its device session."
        )
    }

    private fun currentState(baseUrl: String): PersistedDeviceAuthState? {
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl) ?: return null
        val current = stateStore.load() ?: return null
        if (!current.matchesServerUrl(normalizedBaseUrl)) {
            return null
        }
        return if (current.serverUrl == normalizedBaseUrl) {
            current
        } else {
            current.copy(serverUrl = normalizedBaseUrl)
        }
    }

    private fun clearCachedAccessToken(baseUrl: String) {
        val current = currentState(baseUrl) ?: return
        stateStore.save(current.clearedAccessToken())
    }

    private fun clearSessionCookie(baseUrl: String) {
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl) ?: return
        val uiBaseUrl = normalizedBaseUrl.toHttpUrlOrNull() ?: return
        if (preparedSessionCookieBaseUrl == normalizedBaseUrl) {
            preparedSessionCookieBaseUrl = null
        }
        cookieSession.clearSessionCookie(
            url = apiUrl(uiBaseUrl, "auth", "session"),
            cookieName = SESSION_COOKIE_NAME,
            cookiePath = SESSION_COOKIE_PATH
        )
    }

    private fun clearAccessSessionArtifacts(baseUrl: String) {
        clearCachedAccessToken(baseUrl)
        clearSessionCookie(baseUrl)
    }

    private fun requireReenroll(
        baseUrl: String,
        stage: String,
        error: DeviceAuthHttpException? = null,
        message: String
    ): Nothing {
        clearSessionCookie(baseUrl)
        stateStore.clear()
        telemetry.record(
            DeviceAuthTelemetryEvent(
                name = "device_auth_reenroll_required",
                level = DeviceAuthTelemetryLevel.WARN,
                stage = stage,
                outcome = "clear_device_grant",
                httpStatus = error?.statusCode,
                problemType = error?.problemType
            )
        )
        throw DeviceAuthReenrollRequiredException(message)
    }

    private fun requireSignIn(
        baseUrl: String,
        stage: String,
        error: DeviceAuthHttpException? = null,
        message: String
    ): Nothing {
        clearAccessSessionArtifacts(baseUrl)
        telemetry.record(
            DeviceAuthTelemetryEvent(
                name = "legacy_auth_token_sign_in_required",
                level = DeviceAuthTelemetryLevel.WARN,
                stage = stage,
                outcome = "clear_access_session",
                httpStatus = error?.statusCode,
                problemType = error?.problemType
            )
        )
        throw DeviceAuthSignInRequiredException(message)
    }

    private fun unavailable(
        stage: String,
        message: String,
        error: DeviceAuthHttpException? = null
    ): DeviceAuthUnavailableException {
        telemetry.record(
            DeviceAuthTelemetryEvent(
                name = "device_auth_unavailable",
                level = DeviceAuthTelemetryLevel.WARN,
                stage = stage,
                outcome = "retry_later",
                httpStatus = error?.statusCode,
                problemType = error?.problemType
            )
        )
        return DeviceAuthUnavailableException(message, error)
    }

    private fun normalizedBaseUrl(value: String): String? = ServerTargetResolver.normalizeServerUrl(value)

    private fun resolveTargetPath(uiBaseUrl: HttpUrl, targetUrl: String): String {
        val candidate = targetUrl.toHttpUrlOrNull()
            ?.takeIf { isSameOrigin(it, uiBaseUrl) }
            ?: uiBaseUrl
        val path = candidate.encodedPath.ifBlank { "/" }
        val query = candidate.encodedQuery?.takeIf { it.isNotBlank() }?.let { "?$it" }.orEmpty()
        return "$path$query"
    }

    private fun resolveBootstrapLocation(uiBaseUrl: HttpUrl, locationPath: String?, fallbackTargetPath: String): String {
        if (locationPath.isNullOrBlank()) {
            return uiBaseUrl.resolveAgainst(fallbackTargetPath)
        }
        return uiBaseUrl.resolveAgainst(locationPath)
    }

    private fun apiUrl(baseUrl: HttpUrl, vararg segments: String): HttpUrl =
        baseUrl.newBuilder()
            .encodedPath("/api/v3/")
            .query(null)
            .fragment(null)
            .apply {
                segments.forEach(::addPathSegment)
            }
            .build()

    private fun isSameOrigin(candidate: HttpUrl, baseUrl: HttpUrl): Boolean {
        return candidate.scheme == baseUrl.scheme &&
            candidate.host == baseUrl.host &&
            candidate.port == baseUrl.port
    }

    private fun mergePublishedEndpoints(
        current: List<PublishedEndpoint>,
        refreshed: List<PublishedEndpoint>
    ): List<PublishedEndpoint> {
        if (refreshed.isNotEmpty()) {
            return normalizePublishedEndpoints(refreshed)
        }
        return normalizePublishedEndpoints(current)
    }

    private companion object {
        const val SESSION_COOKIE_NAME = "xg2g_session"
        const val SESSION_COOKIE_PATH = "/api/v3/"
    }
}

internal data class RefreshedDeviceSession(
    val rotatedDeviceGrantId: String? = null,
    val rotatedDeviceGrant: String? = null,
    val accessSessionId: String,
    val accessToken: String,
    val accessTokenExpiresAtEpochMs: Long,
    val policyVersion: String? = null,
    val endpoints: List<PublishedEndpoint> = emptyList()
)

internal data class StartedWebBootstrap(
    val completePath: String,
    val bootstrapToken: String
)

internal data class CompletedWebBootstrap(
    val locationPath: String?
)

internal interface DeviceAuthTransport {
    suspend fun refreshSession(uiBaseUrl: HttpUrl, deviceGrantId: String, deviceGrant: String): RefreshedDeviceSession
    suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String)
    suspend fun startWebBootstrap(uiBaseUrl: HttpUrl, accessToken: String, targetPath: String): StartedWebBootstrap
    suspend fun completeWebBootstrap(uiBaseUrl: HttpUrl, completePath: String, bootstrapToken: String): CompletedWebBootstrap
}

internal class DeviceAuthHttpException(
    val statusCode: Int,
    val problemType: String?,
    override val message: String
) : IllegalStateException(message)

internal class DeviceAuthReenrollRequiredException(
    message: String
) : IllegalStateException(message)

internal class DeviceAuthSignInRequiredException(
    message: String
) : IllegalStateException(message)

internal class DeviceAuthUnavailableException(
    message: String,
    cause: Throwable? = null
) : IllegalStateException(message, cause)

internal enum class DeviceAuthTelemetryLevel {
    INFO,
    WARN
}

internal data class DeviceAuthTelemetryEvent(
    val name: String,
    val level: DeviceAuthTelemetryLevel,
    val stage: String,
    val outcome: String,
    val httpStatus: Int? = null,
    val problemType: String? = null
)

internal interface DeviceAuthTelemetry {
    fun record(event: DeviceAuthTelemetryEvent)
}

private class LogcatDeviceAuthTelemetry : DeviceAuthTelemetry {
    override fun record(event: DeviceAuthTelemetryEvent) {
        val message = buildString {
            append("event=")
            append(event.name)
            append(" stage=")
            append(event.stage)
            append(" outcome=")
            append(event.outcome)
            event.httpStatus?.let {
                append(" httpStatus=")
                append(it)
            }
            event.problemType?.takeIf { it.isNotBlank() }?.let {
                append(" problemType=")
                append(it)
            }
        }
        when (event.level) {
            DeviceAuthTelemetryLevel.INFO -> Log.i(TAG, message)
            DeviceAuthTelemetryLevel.WARN -> Log.w(TAG, message)
        }
    }

    private companion object {
        const val TAG = "Xg2gDeviceAuth"
    }
}

private class OkHttpDeviceAuthTransport(
    private val cookieSession: AuthCookieSession,
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
        val request = Request.Builder()
            .url(apiUrl(uiBaseUrl, "auth", "device", "session"))
            .post(
                JSONObject()
                    .put("deviceGrantId", deviceGrantId)
                    .put("deviceGrant", deviceGrant)
                    .toString()
                    .toRequestBody(JSON_MEDIA_TYPE)
            )
            .build()

        execute(uiBaseUrl, request).use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw response.asDeviceAuthHttpException(body)
            }
            val json = JSONObject(body)
            Log.i(TAG, "action=refresh_session outcome=ok status=${response.code}")
            RefreshedDeviceSession(
                rotatedDeviceGrantId = json.optString("rotatedDeviceGrantId").takeIf { it.isNotBlank() },
                rotatedDeviceGrant = json.optString("rotatedDeviceGrant").takeIf { it.isNotBlank() },
                accessSessionId = json.getString("accessSessionId"),
                accessToken = json.getString("accessToken"),
                accessTokenExpiresAtEpochMs = parseHttpInstant(json.getString("accessTokenExpiresAt")),
                policyVersion = json.optString("policyVersion").takeIf { it.isNotBlank() },
                endpoints = parsePublishedEndpoints(json.optJSONArray("endpoints"))
            )
        }
    }

    override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {
        withContext(Dispatchers.IO) {
            Log.i(TAG, "action=create_cookie_session path=/api/v3/auth/session")
            val request = Request.Builder()
                .url(apiUrl(uiBaseUrl, "auth", "session"))
                .header("Authorization", "Bearer $bearerToken")
                .post(ByteArray(0).toRequestBody(null))
                .build()

            execute(uiBaseUrl, request).use { response ->
                val body = response.body.string()
                if (!response.isSuccessful) {
                    throw response.asDeviceAuthHttpException(body)
                }
                Log.i(TAG, "action=create_cookie_session outcome=ok status=${response.code}")
            }
        }
    }

    override suspend fun startWebBootstrap(
        uiBaseUrl: HttpUrl,
        accessToken: String,
        targetPath: String
    ): StartedWebBootstrap = withContext(Dispatchers.IO) {
        Log.i(TAG, "action=start_web_bootstrap path=/api/v3/auth/web-bootstrap targetPath=$targetPath")
        val request = Request.Builder()
            .url(apiUrl(uiBaseUrl, "auth", "web-bootstrap"))
            .header("Authorization", "Bearer $accessToken")
            .post(
                JSONObject()
                    .put("targetPath", targetPath)
                    .toString()
                    .toRequestBody(JSON_MEDIA_TYPE)
            )
            .build()

        execute(uiBaseUrl, request).use { response ->
            val body = response.body.string()
            if (response.code != 201) {
                throw response.asDeviceAuthHttpException(body)
            }
            val json = JSONObject(body)
            Log.i(TAG, "action=start_web_bootstrap outcome=created status=${response.code}")
            StartedWebBootstrap(
                completePath = json.getString("completePath"),
                bootstrapToken = json.getString("bootstrapToken")
            )
        }
    }

    override suspend fun completeWebBootstrap(
        uiBaseUrl: HttpUrl,
        completePath: String,
        bootstrapToken: String
    ): CompletedWebBootstrap = withContext(Dispatchers.IO) {
        val resolvedUrl = uiBaseUrl.resolve(completePath)
            ?: throw IllegalStateException("Invalid web bootstrap completion path: $completePath")
        Log.i(TAG, "action=complete_web_bootstrap path=${resolvedUrl.encodedPath}")
        val request = Request.Builder()
            .url(resolvedUrl)
            .header(WEB_BOOTSTRAP_HEADER_NAME, bootstrapToken)
            .get()
            .build()

        execute(uiBaseUrl, request).use { response ->
            val body = response.body.string()
            if (response.code !in 300..399) {
                throw response.asDeviceAuthHttpException(body)
            }
            Log.i(TAG, "action=complete_web_bootstrap outcome=redirect status=${response.code}")
            CompletedWebBootstrap(
                locationPath = response.header("Location")
            )
        }
    }

    private fun execute(uiBaseUrl: HttpUrl, request: Request): okhttp3.Response {
        val builder = request.newBuilder()
        cookieSession.applyCookies(request.url, builder)
        val contextualRequest = builder.build().withSameOriginHeaders(uiBaseUrl)
        return okHttpClient.newCall(contextualRequest).execute().also { response ->
            cookieSession.storeCookies(request.url, response.headers)
        }
    }

    private fun apiUrl(baseUrl: HttpUrl, vararg segments: String): HttpUrl =
        baseUrl.newBuilder()
            .encodedPath("/api/v3/")
            .query(null)
            .fragment(null)
            .apply {
                segments.forEach(::addPathSegment)
            }
            .build()

    private fun parseHttpInstant(value: String): Long =
        ZonedDateTime.parse(value, DateTimeFormatter.RFC_1123_DATE_TIME)
            .toInstant()
            .toEpochMilli()

    private fun parsePublishedEndpoints(array: JSONArray?): List<PublishedEndpoint> {
        if (array == null) {
            return emptyList()
        }
        return buildList {
            for (index in 0 until array.length()) {
                val item = array.optJSONObject(index) ?: continue
                add(
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
        }.let(::normalizePublishedEndpoints)
    }

    private fun okhttp3.Response.asDeviceAuthHttpException(body: String): DeviceAuthHttpException {
        val problemType = runCatching {
            if (body.isBlank()) null else JSONObject(body).optString("type").takeIf { it.isNotBlank() }
        }.getOrNull()
        val detail = runCatching {
            if (body.isBlank()) null else JSONObject(body).optString("detail").takeIf { it.isNotBlank() }
        }.getOrNull()
        val message = detail ?: buildString {
            append("HTTP ")
            append(code)
            append(": ")
            append(this@asDeviceAuthHttpException.message)
        }
        return DeviceAuthHttpException(code, problemType, message)
    }

    private companion object {
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        const val WEB_BOOTSTRAP_HEADER_NAME = "X-XG2G-Web-Bootstrap"
        const val TAG = "Xg2gDeviceAuth"
    }
}

internal fun parseDeviceAuthExpiryEpochMs(value: String?): Long? {
    val trimmed = value?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    return trimmed.toLongOrNull()?.takeIf { it > 0L }
        ?: runCatching {
            Instant.from(DateTimeFormatter.RFC_1123_DATE_TIME.parse(trimmed)).toEpochMilli()
        }.getOrNull()
}
