package io.github.manugh.xg2g.android

import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.auth.buildDeviceRefreshRequest
import io.github.manugh.xg2g.android.auth.refreshedSessionFrom
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
        dpopProvider: DPoPProvider = AndroidKeystoreDPoPProvider(),
        transport: DeviceAuthTransport = OkHttpDeviceAuthTransport(cookieSession, dpopProvider)
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
        if (baseUrl.isBlank()) {
            return
        }
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl) ?: return
        val uiBaseUrl = normalizedBaseUrl.toHttpUrlOrNull() ?: return
        val sessionUrl = apiV3Url(uiBaseUrl, "auth", "session")
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
        if (baseUrl.isBlank()) {
            return targetUrl
        }
        val normalizedBaseUrl = normalizedBaseUrl(baseUrl) ?: return targetUrl
        val uiBaseUrl = normalizedBaseUrl.toHttpUrlOrNull() ?: return targetUrl
        val targetPath = resolveTargetPath(uiBaseUrl, targetUrl)
        val sessionUrl = apiV3Url(uiBaseUrl, "auth", "session")
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
                    refreshToken = state.deviceGrant
                )
                // A token rotation says nothing about published endpoints or the
                // policy version, so the stored ones survive it. Overwriting them
                // with the empty values a refresh response does not carry would
                // erase the connectivity the device was paired with.
                val nextState = state.copy(
                    serverUrl = preferredNativeServerUrl(
                        currentServerUrl = uiBaseUrl.toString(),
                        endpoints = state.publishedEndpoints
                    ) ?: uiBaseUrl.toString(),
                    deviceGrantId = refreshed.deviceId,
                    deviceGrant = refreshed.rotatedRefreshToken,
                    accessSessionId = null,
                    accessToken = refreshed.accessToken,
                    accessTokenExpiresAtEpochMs = nowEpochMs() + refreshed.expiresInSeconds * 1000L,
                    policyVersion = state.policyVersion
                )
                stateStore.save(nextState)
                telemetry.record(
                    DeviceAuthTelemetryEvent(
                        name = "device_auth_access_token_ready",
                        level = DeviceAuthTelemetryLevel.INFO,
                        stage = "device_session_refresh",
                        // The refresh token rotates on every call by contract, so
                        // there is no longer a "not rotated" outcome to report.
                        outcome = "refreshed_and_rotated_grant"
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
            url = apiV3Url(uiBaseUrl, "auth", "session"),
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

    private fun isSameOrigin(candidate: HttpUrl, baseUrl: HttpUrl): Boolean {
        return candidate.scheme == baseUrl.scheme &&
            candidate.host == baseUrl.host &&
            candidate.port == baseUrl.port
    }

    private companion object {
        const val SESSION_COOKIE_NAME = "xg2g_session"
        const val SESSION_COOKIE_PATH = "/api/v3/"
    }
}

/**
 * One rotation of the device's credentials.
 *
 * Every field is present because DeviceGrantResponse declares every field
 * required. The previous shape carried nullable rotated-grant fields, an
 * accessSessionId and a published-endpoint list because the retired
 * /auth/device/session response did; carrying them forward would mean writing
 * empty strings and empty lists over state the refresh does not speak about.
 *
 * expiresIn is a lifetime in seconds rather than an instant, so the caller
 * resolves it against its own clock instead of the transport's.
 */
internal data class RefreshedDeviceSession(
    val deviceId: String,
    val accessToken: String,
    val rotatedRefreshToken: String,
    val expiresInSeconds: Int,
    val scope: String
)

internal data class StartedWebBootstrap(
    val completePath: String,
    val bootstrapToken: String
)

internal data class CompletedWebBootstrap(
    val locationPath: String?
)

internal interface DeviceAuthTransport {
    suspend fun refreshSession(uiBaseUrl: HttpUrl, refreshToken: String): RefreshedDeviceSession
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

internal class OkHttpDeviceAuthTransport(
    private val cookieSession: AuthCookieSession,
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

        execute(uiBaseUrl, request).use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw response.asDeviceAuthHttpException(body)
            }
            Log.i(TAG, "action=refresh_session outcome=ok status=${response.code}")
            refreshedSessionFrom(JSONObject(body))
        }
    }

    override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {
        withContext(Dispatchers.IO) {
            Log.i(TAG, "action=create_cookie_session path=/api/v3/auth/session")
            val request = Request.Builder()
                .url(apiV3Url(uiBaseUrl, "auth", "session"))
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
            .url(apiV3Url(uiBaseUrl, "auth", "web-bootstrap"))
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
