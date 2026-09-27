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
import java.time.Instant
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Reports the health of the stream on screen to the backend.
 *
 * One session is watched at a time: the one the viewer is looking at. Its start
 * and end are reported, and in between it is sampled every [sampleIntervalMs].
 * Best-effort, fire-and-forget: failed uploads are dropped and never hold up
 * playback.
 */
internal class PlaybackTelemetryReporter(
    private val scope: CoroutineScope,
    private val sinkProvider: () -> PlaybackTelemetrySink?,
    private val snapshotProvider: () -> PlaybackTelemetrySnapshot,
    private val client: PlaybackTelemetryClient = currentClient(),
    private val sampleIntervalMs: Long = DEFAULT_SAMPLE_INTERVAL_MS,
    private val mainDispatcher: CoroutineDispatcher = Dispatchers.Main,
    private val ioDispatcher: CoroutineDispatcher = Dispatchers.IO,
    private val now: () -> Instant = { Instant.now() }
) {
    private data class Watched(
        val zapId: String,
        val serviceRef: String,
        var previous: PlaybackTelemetrySnapshot,
        var previousAt: Instant
    )

    private var watched: Watched? = null
    private var samplerJob: Job? = null
    private val uploadQueue = Channel<PlaybackTelemetryEvent>(Channel.BUFFERED)

    init {
        // Sequential uploader to guarantee order across session boundaries
        scope.launch(ioDispatcher) {
            for (event in uploadQueue) {
                val sink = sinkProvider() ?: continue
                val batch = PlaybackTelemetryBatch(client = client, events = listOf(event))
                runCatching {
                    sink.send(batch)
                }.onFailure { err ->
                    Log.d(TAG, "telemetry upload failed: ${err.message}")
                }
            }
        }
    }

    /**
     * Starts watching the playback session and reports session_start.
     */
    fun watch(
        zapId: String,
        serviceRef: String,
        startMetrics: Map<String, Double> = emptyMap()
    ) {
        val current = watched
        if (current != null && (current.zapId != zapId || current.serviceRef != serviceRef)) {
            finish("replaced")
        }

        val snapshot = snapshotProvider()
        val takenAt = now()
        watched = Watched(
            zapId = zapId,
            serviceRef = serviceRef,
            previous = snapshot,
            previousAt = takenAt
        )

        emit(
            event(
                kind = PlaybackTelemetryEventKind.SESSION_START,
                zapId = zapId,
                serviceRef = serviceRef,
                metrics = startMetrics.filter { it.value.isFinite() },
                at = takenAt
            )
        )

        samplerJob?.cancel()
        samplerJob = scope.launch(mainDispatcher) {
            while (isActive) {
                delay(sampleIntervalMs)
                if (isActive) {
                    sample()
                }
            }
        }
    }

    /**
     * Takes one sample of the watched session.
     */
    fun sample() {
        val current = watched ?: return
        val snapshot = snapshotProvider()
        val takenAt = now()
        val windowSeconds = (takenAt.toEpochMilli() - current.previousAt.toEpochMilli()) / 1000.0

        val window = PlaybackTelemetryWindow.evaluate(
            previous = current.previous,
            current = snapshot,
            windowSeconds = windowSeconds
        )

        current.previous = snapshot
        current.previousAt = takenAt

        emit(
            event(
                kind = if (window.isDegraded) PlaybackTelemetryEventKind.DEGRADED else PlaybackTelemetryEventKind.HEARTBEAT,
                zapId = current.zapId,
                serviceRef = current.serviceRef,
                metrics = window.metrics,
                reasons = window.reasons,
                detail = window.detail,
                at = takenAt
            )
        )
    }

    /**
     * Reports the end of the session with what it did in its final window.
     */
    fun finish(reason: String) {
        val current = watched ?: return
        stopWatching()

        val finalSnapshot = snapshotProvider()
        val takenAt = now()
        val windowSeconds = (takenAt.toEpochMilli() - current.previousAt.toEpochMilli()) / 1000.0

        val window = PlaybackTelemetryWindow.evaluate(
            previous = current.previous,
            current = finalSnapshot,
            windowSeconds = windowSeconds
        )

        // The closing window of a session is never judged for stalling
        val filteredReasons = window.reasons.filterNot { PlaybackTelemetryWindow.STALL_REASONS.contains(it) }

        emit(
            event(
                kind = PlaybackTelemetryEventKind.SESSION_END,
                zapId = current.zapId,
                serviceRef = current.serviceRef,
                metrics = window.metrics,
                reasons = filteredReasons,
                detail = reason,
                at = takenAt
            )
        )
    }

    fun stopWatching() {
        samplerJob?.cancel()
        samplerJob = null
        watched = null
    }

    private fun event(
        kind: PlaybackTelemetryEventKind,
        zapId: String,
        serviceRef: String,
        metrics: Map<String, Double> = emptyMap(),
        reasons: List<String> = emptyList(),
        detail: String? = null,
        at: Instant? = null
    ): PlaybackTelemetryEvent = PlaybackTelemetryEvent(
        kind = kind,
        occurredAt = at ?: now(),
        detail = detail?.take(512),
        metrics = metrics.takeIf { it.isNotEmpty() },
        reasons = reasons.takeIf { it.isNotEmpty() }?.take(8),
        serviceRef = serviceRef.take(128),
        zapId = zapId.take(64)
    )

    private fun emit(event: PlaybackTelemetryEvent) {
        uploadQueue.trySend(event)
    }

    companion object {
        private const val TAG = "PlaybackTelemetry"
        const val DEFAULT_SAMPLE_INTERVAL_MS = 15_000L

        fun currentClient(): PlaybackTelemetryClient = PlaybackTelemetryClient(
            platform = PlaybackTelemetryClientPlatform.ANDROID,
            appVersion = BuildConfig.VERSION_NAME.take(64),
            build = BuildConfig.VERSION_CODE.toString().take(64),
            device = (Build.MODEL ?: "Android TV").take(64)
        )
    }
}
