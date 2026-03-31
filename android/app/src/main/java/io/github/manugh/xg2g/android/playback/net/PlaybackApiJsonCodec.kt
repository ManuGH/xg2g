package io.github.manugh.xg2g.android.playback.net

import androidx.media3.common.MimeTypes
import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.NativePlaybackDiagnostics
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import io.github.manugh.xg2g.android.playback.model.PlaybackMode
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.SessionState
import org.json.JSONArray
import org.json.JSONObject

internal object PlaybackApiJsonCodec {
    fun playbackCapabilitiesJson(
        capabilities: NativePlaybackCapabilities
    ): String = capabilitiesToJson(capabilities).toString()

    fun recordingPlaybackInfoRequestBody(
        capabilities: NativePlaybackCapabilities
    ): String = capabilitiesToJson(capabilities).toString()

    fun liveDecisionRequestBody(
        request: NativePlaybackRequest.Live,
        capabilities: NativePlaybackCapabilities
    ): String = JSONObject()
        .put("serviceRef", request.serviceRef)
        .put("capabilities", capabilitiesToJson(capabilities))
        .toString()

    fun startLiveIntentRequestBody(
        request: NativePlaybackRequest.Live,
        decision: NativeLiveDecision,
        capabilities: NativePlaybackCapabilities
    ): String {
        val params = request.params.toMutableMap().apply {
            put("playback_mode", decision.playbackMode.wireValue)
            put("playback_decision_token", decision.playbackDecisionToken)
            putIfAbsent("preferred_hls_engine", capabilities.preferredHlsEngine)
            putIfAbsent("device_type", capabilities.deviceType)
            putIfAbsent("client_family", capabilities.clientFamilyFallback)
            decision.capHash?.let { put("capHash", it) }
        }

        return JSONObject()
            .put("type", "stream.start")
            .put("serviceRef", request.serviceRef)
            .put("correlationId", request.correlationId ?: decision.requestId)
            .put("params", JSONObject(params))
            .apply {
                put("playbackDecisionToken", decision.playbackDecisionToken)
                request.hwaccel?.let { put("hwaccel", it) }
            }
            .toString()
    }

    fun stopSessionRequestBody(sessionId: String): String = JSONObject()
        .put("type", "stream.stop")
        .put("sessionId", sessionId)
        .toString()

    fun parseLiveDecisionResponse(
        response: JSONObject,
        capHash: String?
    ): NativeLiveDecision {
        val playbackDecisionToken = response.optString("playbackDecisionToken")
            .takeIf { it.isNotBlank() }
            ?: throw IllegalStateException("Missing playbackDecisionToken in live playback decision")
        val playbackMode = PlaybackMode.fromWireValue(response.optString("mode"))
            ?: throw IllegalStateException("Missing or unsupported mode in live playback decision")

        return NativeLiveDecision(
            requestId = response.optString("requestId").takeIf { it.isNotBlank() },
            playbackDecisionToken = playbackDecisionToken,
            playbackMode = playbackMode,
            capHash = capHash,
            diagnostics = PlaybackJsonCodec.diagnosticsFromLivePlaybackInfo(
                playbackInfo = response,
                playbackMode = playbackMode,
                capHash = capHash
            )
        )
    }

    fun parseStartLiveIntentResponse(
        response: JSONObject,
        diagnostics: NativePlaybackDiagnostics?
    ): NativeLiveStartResult {
        return NativeLiveStartResult(
            sessionId = response.optString("sessionId")
                .takeIf { it.isNotBlank() }
                ?: throw IllegalStateException("Missing sessionId in stream.start response"),
            diagnostics = diagnostics
        )
    }

    fun parseRecordingPlaybackInfoResponse(response: JSONObject): NativeRecordingPlaybackInfo {
        val decision = response.optJSONObject("decision")
            ?: throw IllegalStateException("Missing decision in recording playback info")
        val playbackUrl = decision.optString("selectedOutputUrl")
            .takeIf { it.isNotBlank() }
            ?: throw IllegalStateException("Missing selectedOutputUrl in recording playback info")
        val decisionMode = decision.optString("mode").takeIf { it.isNotBlank() }
        val selectedOutputKind = decision.optString("selectedOutputKind").takeIf { it.isNotBlank() }
        val selected = decision.optJSONObject("selected")
        val container = selected?.optString("container")?.takeIf { it.isNotBlank() }

        return NativeRecordingPlaybackInfo(
            playbackUrl = playbackUrl,
            requestId = response.optString("requestId").takeIf { it.isNotBlank() },
            decisionMode = decisionMode,
            selectedOutputKind = selectedOutputKind,
            mimeType = recordingPlaybackMimeType(selectedOutputKind, container, decisionMode)
        )
    }

    fun heartbeatSnapshot(sessionId: String, response: JSONObject): SessionSnapshot {
        return SessionSnapshot(
            sessionId = sessionId,
            state = SessionState.Active,
            playbackUrl = null,
            mode = null,
            requestId = null,
            profileReason = null,
            traceJson = null,
            heartbeatIntervalSec = null,
            leaseExpiresAt = response.optString("lease_expires_at").takeIf { it.isNotBlank() },
            durationSeconds = null,
            seekableStartSeconds = null,
            seekableEndSeconds = null,
            liveEdgeSeconds = null
        )
    }

    private fun capabilitiesToJson(capabilities: NativePlaybackCapabilities): JSONObject = JSONObject()
        .put("capabilitiesVersion", capabilities.capabilitiesVersion)
        .put("container", JSONArray(capabilities.container))
        .put("videoCodecs", JSONArray(capabilities.videoCodecs))
        .apply {
            if (capabilities.videoCodecSignals.isNotEmpty()) {
                put(
                    "videoCodecSignals",
                    JSONArray(
                        capabilities.videoCodecSignals.map { signal ->
                            JSONObject()
                                .put("codec", signal.codec)
                                .put("supported", signal.supported)
                                .apply {
                                    signal.smooth?.let { put("smooth", it) }
                                    signal.powerEfficient?.let { put("powerEfficient", it) }
                                }
                        }
                    )
                )
            }
            capabilities.maxVideo?.let { maxVideo ->
                put(
                    "maxVideo",
                    JSONObject()
                        .put("width", maxVideo.width)
                        .put("height", maxVideo.height)
                        .apply {
                            maxVideo.fps?.let { put("fps", it) }
                        }
                )
            }
            capabilities.deviceContext?.let { deviceContext ->
                put(
                    "deviceContext",
                    JSONObject()
                        .put("platform", deviceContext.platform)
                        .apply {
                            deviceContext.brand?.let { put("brand", it) }
                            deviceContext.product?.let { put("product", it) }
                            deviceContext.device?.let { put("device", it) }
                            deviceContext.manufacturer?.let { put("manufacturer", it) }
                            deviceContext.model?.let { put("model", it) }
                            put("osName", deviceContext.osName)
                            deviceContext.osVersion?.let { put("osVersion", it) }
                            deviceContext.sdkInt?.let { put("sdkInt", it) }
                        }
                )
            }
            capabilities.networkContext?.let { networkContext ->
                put(
                    "networkContext",
                    JSONObject()
                        .put("kind", networkContext.kind)
                        .apply {
                            networkContext.downlinkKbps?.let { put("downlinkKbps", it) }
                            networkContext.metered?.let { put("metered", it) }
                            networkContext.internetValidated?.let { put("internetValidated", it) }
                        }
                )
            }
        }
        .put("audioCodecs", JSONArray(capabilities.audioCodecs))
        .put("supportsHls", capabilities.supportsHls)
        .put("supportsRange", capabilities.supportsRange)
        .put("deviceType", capabilities.deviceType)
        .put("hlsEngines", JSONArray(capabilities.hlsEngines))
        .put("preferredHlsEngine", capabilities.preferredHlsEngine)
        .put("runtimeProbeUsed", capabilities.runtimeProbeUsed)
        .put("runtimeProbeVersion", capabilities.runtimeProbeVersion)
        .put("clientFamilyFallback", capabilities.clientFamilyFallback)
        .put("allowTranscode", capabilities.allowTranscode)
}

internal fun recordingPlaybackMimeType(selectedOutputKind: String?, container: String?, decisionMode: String? = null): String? {
    val directFile = selectedOutputKind == "file" || decisionMode?.trim()?.lowercase() == "direct_play"
    if (!directFile) {
        return null
    }

    return when (container?.trim()?.lowercase()) {
        "ts", "mpegts" -> MimeTypes.VIDEO_MP2T
        "mp4", "m4v", "mov" -> MimeTypes.VIDEO_MP4
        else -> null
    }
}

internal data class NativeLiveDecision(
    val requestId: String?,
    val playbackDecisionToken: String,
    val playbackMode: PlaybackMode,
    val capHash: String?,
    val diagnostics: NativePlaybackDiagnostics?
)
