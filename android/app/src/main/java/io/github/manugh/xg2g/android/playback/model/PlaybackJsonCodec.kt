package io.github.manugh.xg2g.android.playback.model

import org.json.JSONObject

internal object PlaybackJsonCodec {
    fun requestToJson(request: NativePlaybackRequest): String =
        requestToJsonObject(request, includeAuthToken = true).toString()

    private fun requestToJsonObject(
        request: NativePlaybackRequest,
        includeAuthToken: Boolean
    ): JSONObject = when (request) {
        is NativePlaybackRequest.Live -> JSONObject()
            .put("kind", "live")
            .put("serviceRef", request.serviceRef)
            .put("playbackDecisionToken", request.playbackDecisionToken)
            .put("hwaccel", request.hwaccel)
            .put("title", request.title)
            .put("correlationId", request.correlationId)
            .put("params", JSONObject(request.params))
            .applyOptionalAuthToken(request.authToken, includeAuthToken)

        is NativePlaybackRequest.Recording -> JSONObject()
            .put("kind", "recording")
            .put("recordingId", request.recordingId)
            .put("startPositionMs", request.startPositionMs)
            .put("title", request.title)
            .put("correlationId", request.correlationId)
            .applyOptionalAuthToken(request.authToken, includeAuthToken)
    }

    fun requestFromJson(json: String): NativePlaybackRequest {
        val obj = JSONObject(json)
        return when (obj.optString("kind")) {
            "live" -> NativePlaybackRequest.Live(
                serviceRef = obj.optString("serviceRef")
                    .takeIf { it.isNotBlank() }
                    ?: throw IllegalArgumentException("Missing serviceRef"),
                playbackDecisionToken = obj.optNullableString("playbackDecisionToken"),
                hwaccel = obj.optNullableString("hwaccel"),
                params = obj.optJSONObject("params")
                    ?.toStringMap()
                    .orEmpty(),
                title = obj.optNullableString("title"),
                correlationId = obj.optNullableString("correlationId"),
                authToken = obj.optNullableString("authToken")
            )

            "recording" -> NativePlaybackRequest.Recording(
                recordingId = obj.optString("recordingId")
                    .takeIf { it.isNotBlank() }
                    ?: throw IllegalArgumentException("Missing recordingId"),
                startPositionMs = obj.optLong("startPositionMs", 0L),
                title = obj.optNullableString("title"),
                correlationId = obj.optNullableString("correlationId"),
                authToken = obj.optNullableString("authToken")
            )

            else -> throw IllegalArgumentException("Unsupported native playback kind")
        }
    }

    fun stateToJson(state: NativePlaybackState): String = JSONObject()
        .put(
            "activeRequest",
            state.activeRequest?.let { requestToJsonObject(it, includeAuthToken = false) }
        )
        .put("session", state.session?.let(::sessionToJsonObject))
        .put("diagnostics", state.diagnostics?.let(::diagnosticsToJsonObject))
        .put("playerState", state.playerState)
        .put("playWhenReady", state.playWhenReady)
        .put("isInPip", state.isInPip)
        .put("lastError", state.lastError)
        .toString()

    fun sessionFromJson(json: JSONObject, fallbackSessionId: String? = null): SessionSnapshot {
        val heartbeatInterval =
            if (json.has("heartbeat_interval")) json.optInt("heartbeat_interval")
            else if (json.has("heartbeatInterval")) json.optInt("heartbeatInterval")
            else 0

        return SessionSnapshot(
            sessionId = json.optString("sessionId")
                .takeIf { it.isNotBlank() }
                ?: fallbackSessionId
                ?: throw IllegalArgumentException("Missing sessionId"),
            state = SessionState.fromWireValue(json.optString("state").ifBlank { null }),
            playbackUrl = json.optNullableString("playbackUrl"),
            mode = SessionMode.fromWireValue(json.optNullableString("mode")),
            requestId = json.optNullableString("requestId"),
            profileReason = json.optNullableString("profileReason"),
            traceJson = json.optJSONObject("trace")?.toString(),
            heartbeatIntervalSec = heartbeatInterval.takeIf { it > 0 },
            leaseExpiresAt = json.optNullableString("lease_expires_at")
                ?: json.optNullableString("leaseExpiresAt"),
            durationSeconds = json.optNullableDouble("durationSeconds"),
            seekableStartSeconds = json.optNullableDouble("seekableStartSeconds"),
            seekableEndSeconds = json.optNullableDouble("seekableEndSeconds"),
            liveEdgeSeconds = json.optNullableDouble("liveEdgeSeconds")
        )
    }

    fun diagnosticsFromLivePlaybackInfo(
        playbackInfo: JSONObject,
        playbackMode: PlaybackMode,
        capHash: String?
    ): NativePlaybackDiagnostics {
        val traceJson = playbackInfo.optJSONObject("decision")
            ?.optJSONObject("trace")
            ?.toString()

        return NativePlaybackDiagnostics(
            requestId = playbackInfo.optNullableString("requestId"),
            playbackMode = playbackMode,
            profileReason = playbackInfo.optNullableString("reason"),
            capHash = capHash,
            playbackInfoJson = playbackInfo.toString(),
            traceJson = traceJson
        )
    }

    private fun sessionToJsonObject(session: SessionSnapshot): JSONObject = JSONObject()
        .put("sessionId", session.sessionId)
        .put("state", session.state.wireValue)
        .put("playbackUrl", session.playbackUrl)
        .put("mode", session.mode?.wireValue)
        .put("requestId", session.requestId)
        .put("profileReason", session.profileReason)
        .put("trace", session.traceJson?.let(::JSONObject))
        .put("heartbeatIntervalSec", session.heartbeatIntervalSec)
        .put("leaseExpiresAt", session.leaseExpiresAt)
        .put("durationSeconds", session.durationSeconds)
        .put("seekableStartSeconds", session.seekableStartSeconds)
        .put("seekableEndSeconds", session.seekableEndSeconds)
        .put("liveEdgeSeconds", session.liveEdgeSeconds)

    private fun diagnosticsToJsonObject(diagnostics: NativePlaybackDiagnostics): JSONObject = JSONObject()
        .put("requestId", diagnostics.requestId)
        .put("playbackMode", diagnostics.playbackMode?.wireValue)
        .put("profileReason", diagnostics.profileReason)
        .put("capHash", diagnostics.capHash)
        .put("playbackInfo", diagnostics.playbackInfoJson?.let(::JSONObject))
        .put("trace", diagnostics.traceJson?.let(::JSONObject))
}

internal fun JSONObject.optNullableString(name: String): String? =
    optString(name).takeIf { it.isNotBlank() && it != "null" }

private fun JSONObject.applyOptionalAuthToken(
    authToken: String?,
    includeAuthToken: Boolean
): JSONObject = apply {
    if (includeAuthToken) {
        put("authToken", authToken)
    }
}

private fun JSONObject.optNullableDouble(name: String): Double? =
    if (has(name) && !isNull(name)) optDouble(name) else null

private fun JSONObject.toStringMap(): Map<String, String> = buildMap {
    for (key in keys()) {
        put(key, optString(key))
    }
}
