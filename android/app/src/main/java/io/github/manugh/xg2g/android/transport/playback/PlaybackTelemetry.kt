// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package io.github.manugh.xg2g.android.transport.playback

import android.os.Build
import android.util.Log
import io.github.manugh.xg2g.android.BuildConfig
import io.github.manugh.xg2g.android.contract.PlaybackTelemetryBatch
import io.github.manugh.xg2g.android.contract.PlaybackTelemetryClient
import io.github.manugh.xg2g.android.contract.PlaybackTelemetryClientPlatform
import io.github.manugh.xg2g.android.contract.PlaybackTelemetryEvent
import io.github.manugh.xg2g.android.contract.PlaybackTelemetryEventKind
import io.github.manugh.xg2g.android.transport.apiV3Url
import java.time.Instant
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

/**
 * Where playback telemetry goes. A seam so the reporter can be tested without a
 * server.
 */
internal interface PlaybackTelemetrySink {
    suspend fun send(batch: PlaybackTelemetryBatch)
}

/**
 * Sends playback telemetry to the backend at POST /api/v3/telemetry/playback.
 */
internal class HTTPPlaybackTelemetrySink(
    private val okHttpClient: OkHttpClient,
    private val baseUrlProvider: () -> HttpUrl?,
    private val clientIdProvider: () -> String = { "android-${Build.MODEL}" },
    private val ioDispatcher: CoroutineDispatcher = Dispatchers.IO
) : PlaybackTelemetrySink {

    override suspend fun send(batch: PlaybackTelemetryBatch): Unit = withContext(ioDispatcher) {
        val baseUrl = baseUrlProvider() ?: return@withContext
        val url = apiV3Url(baseUrl, "telemetry", "playback")
        val jsonString = batch.toJson().toString()
        val request = Request.Builder()
            .url(url)
            .post(jsonString.toRequestBody(JSON_MEDIA_TYPE))
            .header("Content-Type", "application/json")
            .header("X-Xg2g-Client-Id", clientIdProvider().take(64))
            .build()
            .withSameOriginHeaders(baseUrl)

        runCatching {
            okHttpClient.newCall(request).execute().use { response ->
                if (!response.isSuccessful) {
                    Log.w(TAG, "failed to send playback telemetry batch: HTTP ${response.code}")
                }
            }
        }.onFailure { err ->
            Log.w(TAG, "failed to execute playback telemetry request: ${err.message}")
        }
    }

    private companion object {
        const val TAG = "PlaybackTelemetrySink"
        val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}

/**
 * Cumulative figures snapshot of the playback session on screen.
 */
internal data class PlaybackTelemetrySnapshot(
    val bytesReceivedTotal: Long = 0L,
    val decodedFramesTotal: Long = 0L,
    val presentedFramesTotal: Long = 0L,
    val audioUnderrunsTotal: Long = 0L,
    val networkStallsTotal: Long = 0L,
    val longestNetworkStallMs: Double = 0.0,
    val decodeErrorsTotal: Long = 0L,
    val decoderRecoveriesTotal: Long = 0L,
    val droppedFramesTotal: Long = 0L,
    val warnings: List<String> = emptyList(),
    val ttfpMs: Double? = null
)

/**
 * What one sampling window says about the stream on screen.
 *
 * Pure: two snapshots of the same session in, one verdict out. Judgements are
 * made strictly on cumulative counters, never on a rate.
 */
internal data class PlaybackTelemetryWindow(
    val reasons: List<String>,
    val metrics: Map<String, Double>,
    val detail: String?
) {
    val isDegraded: Boolean get() = reasons.isNotEmpty()

    companion object {
        const val AUDIO_UNDERRUN_THRESHOLD = 10
        private val METRIC_KEY_REGEX = Regex("^[a-z][A-Za-z0-9]{0,47}$")
        val STALL_REASONS = setOf("no_input", "video_decode_stalled", "presentation_stalled")

        fun evaluate(
            previous: PlaybackTelemetrySnapshot?,
            current: PlaybackTelemetrySnapshot,
            windowSeconds: Double
        ): PlaybackTelemetryWindow {
            val prev = previous ?: PlaybackTelemetrySnapshot()

            val input = delta(current.bytesReceivedTotal, prev.bytesReceivedTotal)
            val decoded = delta(current.decodedFramesTotal, prev.decodedFramesTotal)
            val presented = delta(current.presentedFramesTotal, prev.presentedFramesTotal)
            val underruns = delta(current.audioUnderrunsTotal, prev.audioUnderrunsTotal)
            val stalls = delta(current.networkStallsTotal, prev.networkStallsTotal)
            val decodeErrors = delta(current.decodeErrorsTotal, prev.decodeErrorsTotal)
            val dropped = delta(current.droppedFramesTotal, prev.droppedFramesTotal)
            val newWarnings = if (current.warnings.size > prev.warnings.size) {
                current.warnings.subList(prev.warnings.size, current.warnings.size)
            } else {
                emptyList()
            }

            val reasons = mutableListOf<String>()
            if (previous != null) {
                if (input == 0L) {
                    reasons.add("no_input")
                } else if (decoded == 0L) {
                    reasons.add("video_decode_stalled")
                } else if (presented == 0L) {
                    reasons.add("presentation_stalled")
                }
            }
            if (underruns >= AUDIO_UNDERRUN_THRESHOLD) {
                reasons.add("audio_underruns")
            }
            if (stalls > 0L) {
                reasons.add("network_stalls")
            }
            if (decodeErrors > 0L) {
                reasons.add("decode_errors")
            }
            if (newWarnings.isNotEmpty()) {
                reasons.add("pipeline_warning")
            }

            val rawMetrics = mutableMapOf<String, Double>(
                "windowSeconds" to windowSeconds,
                "bytesReceivedDelta" to input.toDouble(),
                "decodedFramesDelta" to decoded.toDouble(),
                "presentedFieldsDelta" to presented.toDouble(),
                "audioUnderrunsDelta" to underruns.toDouble(),
                "networkStallsDelta" to stalls.toDouble(),
                "decodeErrorsDelta" to decodeErrors.toDouble(),
                "droppedFramesDelta" to dropped.toDouble(),
                "audioUnderruns" to current.audioUnderrunsTotal.toDouble(),
                "networkStalls" to current.networkStallsTotal.toDouble(),
                "longestNetworkStallMs" to current.longestNetworkStallMs,
                "decodeErrors" to current.decodeErrorsTotal.toDouble(),
                "decoderRecoveries" to current.decoderRecoveriesTotal.toDouble(),
                "droppedFrames" to current.droppedFramesTotal.toDouble()
            )
            current.ttfpMs?.let { if (it > 0) rawMetrics["ttfpTotalMs"] = it }

            val filteredMetrics = rawMetrics
                .filter { (key, value) -> METRIC_KEY_REGEX.matches(key) && value.isFinite() }

            val detail = newWarnings.lastOrNull()?.take(512)
            return PlaybackTelemetryWindow(reasons = reasons, metrics = filteredMetrics, detail = detail)
        }

        private fun delta(current: Long, previous: Long): Long =
            if (current >= previous) current - previous else current
    }
}
