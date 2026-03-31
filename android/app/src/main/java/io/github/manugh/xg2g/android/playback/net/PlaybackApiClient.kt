package io.github.manugh.xg2g.android.playback.net

import android.content.Context
import android.util.Log
import android.util.Base64
import io.github.manugh.xg2g.android.ServerSettingsStore
import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.PlaybackMode
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.session.PlaybackErrorMapper
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import org.json.JSONObject
import java.nio.charset.StandardCharsets

internal class PlaybackApiClient(
    context: Context,
    private val serverSettingsStore: ServerSettingsStore = ServerSettingsStore(context.applicationContext),
    private val errorMapper: PlaybackErrorMapper = PlaybackErrorMapper(),
    private val cookieSession: CookieBackedAuthSession = CookieBackedAuthSession(),
    private val nativeCapabilities: NativePlaybackCapabilities = NativePlaybackCapabilities.create(context.applicationContext),
    val okHttpClient: OkHttpClient = OkHttpClient.Builder()
        .addNetworkInterceptor { chain ->
            val original = chain.request()
            val builder = original.newBuilder()
            cookieSession.applyCookies(original.url, builder)
            val response = chain.proceed(builder.build())
            cookieSession.storeCookies(original.url, response.headers)
            response
        }
        .build()
) : PlaybackApi {

    override suspend fun ensureAuthSession(authToken: String?) {
        withContext(Dispatchers.IO) {
            val sessionUrl = apiUrl("auth", "session")
            val bearerToken = authToken?.trim().takeIf { !it.isNullOrEmpty() }
            Log.d(
                TAG,
                "ensureAuthSession path=${sessionUrl.encodedPath} hasBearer=${bearerToken != null} hasSessionCookieBefore=${cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)}"
            )
            if (bearerToken == null && cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)) {
                return@withContext
            }

            val requestBuilder = Request.Builder()
                .url(sessionUrl)
                .post(ByteArray(0).toRequestBody(null))

            if (bearerToken != null) {
                requestBuilder.header("Authorization", "Bearer $bearerToken")
            }

            val request = requestBuilder.build()

            execute(request).use { response ->
                Log.d(
                    TAG,
                    "ensureAuthSession response code=${response.code} hasSetCookie=${response.headers.values("Set-Cookie").isNotEmpty()} hasSessionCookieAfter=${cookieSession.hasSessionCookie(sessionUrl, SESSION_COOKIE_NAME)}"
                )
                if (!response.isSuccessful) {
                    throw errorMapper.toHttpException(response, response.body.string())
                }
            }
        }
    }

    override suspend fun startLiveIntent(request: NativePlaybackRequest.Live): NativeLiveStartResult = withContext(Dispatchers.IO) {
        val decision = if (!request.playbackDecisionToken.isNullOrBlank()) {
            NativeLiveDecision(
                requestId = request.correlationId,
                playbackDecisionToken = request.playbackDecisionToken,
                playbackMode = PlaybackMode.fromWireValue(request.params["playback_mode"])
                    ?: throw IllegalStateException(
                        "Missing or unsupported playback_mode for live request with precomputed decision"
                    ),
                capHash = request.params["capHash"] ?: extractCapHashFromDecisionToken(request.playbackDecisionToken),
                diagnostics = null,
            )
        } else {
            requestLiveDecision(request)
        }

        val httpRequest = Request.Builder()
            .url(apiUrl("intents"))
            .post(
                PlaybackApiJsonCodec.startLiveIntentRequestBody(
                    request = request,
                    decision = decision,
                    capabilities = nativeCapabilities
                ).toRequestBody(JSON_MEDIA_TYPE)
            )
            .build()

        executeJson(httpRequest) { response ->
            PlaybackApiJsonCodec.parseStartLiveIntentResponse(response, decision.diagnostics)
        }
    }

    override suspend fun getSessionState(sessionId: String): SessionSnapshot = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url(apiUrl("sessions", sessionId))
            .get()
            .build()

        executeJson(request) { response -> PlaybackJsonCodec.sessionFromJson(response, fallbackSessionId = sessionId) }
    }

    override suspend fun getRecordingPlaybackInfo(request: NativePlaybackRequest.Recording): NativeRecordingPlaybackInfo? = withContext(Dispatchers.IO) {
        val httpRequest = Request.Builder()
            .url(apiUrl("recordings", request.recordingId, "stream-info"))
            .post(
                PlaybackApiJsonCodec.recordingPlaybackInfoRequestBody(
                    capabilities = nativeCapabilities
                ).toRequestBody(JSON_MEDIA_TYPE)
            )
            .build()

        execute(httpRequest).use { response ->
            val body = response.body.string()
            when {
                response.isSuccessful -> {
                    val json = if (body.isBlank()) JSONObject() else JSONObject(body)
                    val playbackInfo = PlaybackApiJsonCodec.parseRecordingPlaybackInfoResponse(json)
                    val normalizedPlaybackUrl = normalizeRecordingPlaybackUrl(
                        uiBaseUrl = requireUiBaseUrl(),
                        recordingId = request.recordingId,
                        playbackUrl = playbackInfo.playbackUrl,
                        profile = ANDROID_RECORDING_PROFILE,
                        selectedOutputKind = playbackInfo.selectedOutputKind,
                        decisionMode = playbackInfo.decisionMode
                    )
                    if (normalizedPlaybackUrl != playbackInfo.playbackUrl) {
                        Log.d(
                            TAG,
                            "rewriting native recording playback url from=${playbackInfo.playbackUrl} to=$normalizedPlaybackUrl"
                        )
                    }
                    playbackInfo.copy(playbackUrl = normalizedPlaybackUrl)
                }
                response.code == 503 -> null
                else -> throw errorMapper.toHttpException(response, body)
            }
        }
    }

    override suspend fun getRecordingPlaylistIfReady(recordingId: String): String? = withContext(Dispatchers.IO) {
        val playlistUrl = recordingPlaylistHttpUrl(recordingId)
        val request = Request.Builder()
            .url(playlistUrl)
            .head()
            .build()

        execute(request).use { response ->
            when {
                response.isSuccessful -> playlistUrl.toString()
                response.code == 503 -> null
                else -> throw errorMapper.toHttpException(response, response.body.string())
            }
        }
    }

    override suspend fun getPlaybackUrlIfReady(playbackUrl: String): String? = withContext(Dispatchers.IO) {
        val resolvedUrl = resolvePlaybackUrl(playbackUrl)
        val request = Request.Builder()
            .url(resolvedUrl)
            .head()
            .build()

        execute(request).use { response ->
            when {
                response.isSuccessful -> resolvedUrl
                response.code == 503 -> null
                else -> throw errorMapper.toHttpException(response, response.body.string())
            }
        }
    }

    override suspend fun heartbeat(sessionId: String): SessionSnapshot = withContext(Dispatchers.IO) {
        val request = Request.Builder()
            .url(apiUrl("sessions", sessionId, "heartbeat"))
            .post(ByteArray(0).toRequestBody(null))
            .build()

        executeJson(request) { response -> PlaybackApiJsonCodec.heartbeatSnapshot(sessionId, response) }
    }

    override suspend fun stopSession(sessionId: String) {
        withContext(Dispatchers.IO) {
            val request = Request.Builder()
                .url(apiUrl("intents"))
                .post(PlaybackApiJsonCodec.stopSessionRequestBody(sessionId).toRequestBody(JSON_MEDIA_TYPE))
                .build()

            execute(request).use { response ->
                if (!response.isSuccessful) {
                    throw errorMapper.toHttpException(response, response.body.string())
                }
            }
        }
    }

    override fun sessionPlaylistUrl(sessionId: String): String =
        apiUrl("sessions", sessionId, "hls", "index.m3u8").toString()

    override fun recordingPlaylistUrl(recordingId: String): String =
        recordingPlaylistHttpUrl(recordingId).toString()

    fun playbackRequestHeaders(playbackUrl: String): Map<String, String> {
        val resolvedUrl = resolvePlaybackUrl(playbackUrl).toHttpUrlOrNull() ?: requireUiBaseUrl()
        return playbackRequestHeaders(
            uiBaseUrl = requireUiBaseUrl(),
            cookieHeader = cookieSession.cookieHeader(resolvedUrl)
        )
    }

    private fun recordingPlaylistHttpUrl(recordingId: String): HttpUrl =
        recordingPlaylistHttpUrl(
            apiUrl = apiUrl("recordings", recordingId, "playlist.m3u8"),
            profile = ANDROID_RECORDING_PROFILE
        )

    fun resolvePlaybackUrl(url: String): String =
        requireUiBaseUrl().resolveAgainst(url)

    private fun execute(request: Request): Response {
        val contextualRequest = request.withSameOriginHeaders(requireUiBaseUrl())
        Log.d(
            TAG,
            "execute request method=${contextualRequest.method} path=${contextualRequest.url.encodedPath} hasCookie=${contextualRequest.header("Cookie") != null} hasAuthorization=${contextualRequest.header("Authorization") != null} hasOrigin=${contextualRequest.header("Origin") != null} hasReferer=${contextualRequest.header("Referer") != null}"
        )
        return okHttpClient.newCall(contextualRequest).execute().also { response ->
            Log.d(
                TAG,
                "execute response method=${contextualRequest.method} path=${contextualRequest.url.encodedPath} code=${response.code}"
            )
        }
    }

    private fun requestLiveDecision(request: NativePlaybackRequest.Live): NativeLiveDecision {
        val httpRequest = Request.Builder()
            .url(apiUrl("live", "stream-info"))
            .post(
                PlaybackApiJsonCodec.liveDecisionRequestBody(
                    request = request,
                    capabilities = nativeCapabilities
                ).toRequestBody(JSON_MEDIA_TYPE)
            )
            .build()

        return executeJson(httpRequest) { response ->
            val playbackDecisionToken = response.optString("playbackDecisionToken")
            PlaybackApiJsonCodec.parseLiveDecisionResponse(
                response = response,
                capHash = extractCapHashFromDecisionToken(playbackDecisionToken)
            )
        }
    }

    private fun <T> executeJson(request: Request, transform: (JSONObject) -> T): T {
        execute(request).use { response ->
            val body = response.body.string()
            if (!response.isSuccessful) {
                throw errorMapper.toHttpException(response, body)
            }

            val json = if (body.isBlank()) JSONObject() else JSONObject(body)
            return transform(json)
        }
    }

    private fun requireUiBaseUrl(): HttpUrl {
        val uiBaseUrl = serverSettingsStore.getServerUrl()
            ?: throw IllegalStateException("No xg2g server configured for native playback")
        return uiBaseUrl.toHttpUrlOrNull()
            ?: throw IllegalStateException("Invalid xg2g server URL: $uiBaseUrl")
    }

    private fun apiUrl(vararg segments: String): HttpUrl {
        val uiBase = requireUiBaseUrl()
        return uiBase.newBuilder()
            .encodedPath("/api/v3/")
            .query(null)
            .fragment(null)
            .apply {
                segments.forEach(::addPathSegment)
            }
            .build()
    }

    private fun extractCapHashFromDecisionToken(token: String?): String? {
        if (token.isNullOrBlank()) {
            return null
        }

        val segments = token.split('.')
        if (segments.size < 2) {
            return null
        }

        return runCatching {
            val payload = String(
                Base64.decode(
                    segments[1],
                    Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING
                ),
                StandardCharsets.UTF_8
            )
            JSONObject(payload).optString("capHash").takeIf { it.isNotBlank() }
        }.getOrNull()
    }

    private companion object {
        const val TAG = "Xg2gPlaybackApi"
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
        const val SESSION_COOKIE_NAME = "xg2g_session"
        const val ANDROID_RECORDING_PROFILE = "android_native"
    }
}

internal fun recordingPlaylistHttpUrl(apiUrl: HttpUrl, profile: String): HttpUrl =
    apiUrl.newBuilder()
        .addQueryParameter("profile", profile)
        .build()

internal fun normalizeRecordingPlaybackUrl(
    uiBaseUrl: HttpUrl,
    recordingId: String,
    playbackUrl: String,
    profile: String,
    selectedOutputKind: String? = null,
    decisionMode: String? = null
): String {
    val resolvedUrl = uiBaseUrl.resolve(playbackUrl) ?: playbackUrl.toHttpUrlOrNull()
    if (resolvedUrl?.encodedPath?.endsWith("/stream.mp4") != true) {
        return playbackUrl
    }
    if (selectedOutputKind == "file" || decisionMode?.trim()?.lowercase() == "direct_play") {
        return playbackUrl
    }

    val playlistApiUrl = uiBaseUrl.newBuilder()
        .encodedPath("/api/v3/")
        .query(null)
        .fragment(null)
        .addPathSegment("recordings")
        .addPathSegment(recordingId)
        .addPathSegment("playlist.m3u8")
        .build()

    return recordingPlaylistHttpUrl(
        apiUrl = playlistApiUrl,
        profile = profile
    ).toString()
}

internal fun Request.withSameOriginHeaders(uiBaseUrl: HttpUrl): Request {
    val origin = uiBaseUrl.originHeaderValue()
    val referer = uiBaseUrl.toString()
    return newBuilder().apply {
        if (header("Origin").isNullOrBlank()) {
            header("Origin", origin)
        }
        if (header("Referer").isNullOrBlank()) {
            header("Referer", referer)
        }
    }.build()
}

internal fun HttpUrl.originHeaderValue(): String =
    "${scheme}://${host}${portSuffixForOrigin()}"

internal fun HttpUrl.resolveAgainst(target: String): String =
    resolve(target)?.toString() ?: target

internal fun playbackRequestHeaders(
    uiBaseUrl: HttpUrl,
    cookieHeader: String?
): Map<String, String> = buildMap {
    put("Origin", uiBaseUrl.originHeaderValue())
    put("Referer", uiBaseUrl.toString())
    cookieHeader
        ?.takeIf { it.isNotBlank() }
        ?.let { put("Cookie", it) }
}

private fun HttpUrl.portSuffixForOrigin(): String {
    val defaultPort = when (scheme) {
        "http" -> 80
        "https" -> 443
        else -> -1
    }
    return if (port == defaultPort) "" else ":$port"
}
