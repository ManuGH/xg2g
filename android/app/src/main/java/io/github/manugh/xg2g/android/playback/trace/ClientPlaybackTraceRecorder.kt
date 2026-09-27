package io.github.manugh.xg2g.android.playback.trace

import android.os.SystemClock
import io.github.manugh.xg2g.android.contract.ClientPlaybackTraceBatch
import io.github.manugh.xg2g.android.contract.ClientPlaybackTraceEvent
import io.github.manugh.xg2g.android.contract.ClientPlaybackTraceEventEvent
import io.github.manugh.xg2g.android.contract.ClientPlaybackTraceEventStage
import java.time.Instant
import java.util.ArrayDeque

/** Keeps a small, recent playback timeline for best-effort anomaly reporting. */
internal class ClientPlaybackTraceRecorder(
    private val elapsedRealtimeMs: () -> Long = SystemClock::elapsedRealtime,
    private val wallClock: () -> Instant = Instant::now,
    private val maxEvents: Int = MAX_EVENTS,
    private val windowMs: Long = WINDOW_MS
) {
    private val events = ArrayDeque<ClientPlaybackTraceEvent>()
    private var playbackStartedAtMs: Long? = null
    private var nextSequence = 1L
    private var lastAnomalySequence = 0L

    @Synchronized
    fun startPlayback() {
        events.clear()
        nextSequence = 1L
        lastAnomalySequence = 0L
        playbackStartedAtMs = elapsedRealtimeMs()
        append(ClientPlaybackTraceEventEvent.PLAYBACK_STARTED, valueMs = null)
    }

    @Synchronized
    fun endPlayback() {
        events.clear()
        playbackStartedAtMs = null
        lastAnomalySequence = 0L
    }

    /** Returns true when this event should trigger a best-effort upload. */
    @Synchronized
    fun record(event: ClientPlaybackTraceEventEvent, valueMs: Double? = null): Boolean {
        if (playbackStartedAtMs == null) return false
        append(event, valueMs)
        return (event in ANOMALY_EVENTS).also { isAnomaly ->
            if (isAnomaly) lastAnomalySequence = nextSequence - 1
        }
    }

    @Synchronized
    fun hasAnomalyAfter(sequence: Long): Boolean = lastAnomalySequence > sequence

    @Synchronized
    fun snapshot(): ClientPlaybackTraceBatch? {
        if (events.isEmpty()) return null
        prune(elapsedRealtimeMs())
        if (events.isEmpty()) return null
        return ClientPlaybackTraceBatch(events = events.toList(), observedAt = wallClock())
    }

    private fun append(event: ClientPlaybackTraceEventEvent, valueMs: Double?) {
        val startedAt = playbackStartedAtMs ?: return
        val elapsed = (elapsedRealtimeMs() - startedAt).coerceAtLeast(0L)
        prune(elapsedRealtimeMs())
        events.addLast(
            ClientPlaybackTraceEvent(
                elapsedMs = elapsed,
                event = event,
                sequence = nextSequence++,
                stage = event.stage,
                valueMs = valueMs?.takeIf { it.isFinite() && it in 0.0..MAX_VALUE_MS }
            )
        )
        while (events.size > maxEvents) events.removeFirst()
    }

    private fun prune(nowElapsedMs: Long) {
        val startedAt = playbackStartedAtMs ?: return
        val elapsed = (nowElapsedMs - startedAt).coerceAtLeast(0L)
        while (events.isNotEmpty() && elapsed - events.first().elapsedMs > windowMs) {
            events.removeFirst()
        }
    }

    private val ClientPlaybackTraceEventEvent.stage: ClientPlaybackTraceEventStage
        get() = when (this) {
            ClientPlaybackTraceEventEvent.PLAYBACK_STARTED -> ClientPlaybackTraceEventStage.LIFECYCLE
            ClientPlaybackTraceEventEvent.REQUEST_STARTED,
            ClientPlaybackTraceEventEvent.HTTP_RESPONSE,
            ClientPlaybackTraceEventEvent.FIRST_BYTE,
            ClientPlaybackTraceEventEvent.TRANSPORT_GAP,
            ClientPlaybackTraceEventEvent.STREAM_CLOSED -> ClientPlaybackTraceEventStage.NETWORK
            ClientPlaybackTraceEventEvent.PSI_READY -> ClientPlaybackTraceEventStage.DEMUX
            ClientPlaybackTraceEventEvent.VIDEO_PARAMETERS_READY,
            ClientPlaybackTraceEventEvent.FIRST_IDR,
            ClientPlaybackTraceEventEvent.FIRST_DECODED_FRAME,
            ClientPlaybackTraceEventEvent.DECODE_ERROR,
            ClientPlaybackTraceEventEvent.DECODER_RECOVERY -> ClientPlaybackTraceEventStage.DECODE
            ClientPlaybackTraceEventEvent.CONTINUITY_ERROR,
            ClientPlaybackTraceEventEvent.PTS_DISCONTINUITY,
            ClientPlaybackTraceEventEvent.PES_ERROR -> ClientPlaybackTraceEventStage.TRANSPORT
            ClientPlaybackTraceEventEvent.AUDIO_UNDERRUN,
            ClientPlaybackTraceEventEvent.AUDIO_CLOCK_STARTED,
            ClientPlaybackTraceEventEvent.AUDIO_CLOCK_STOPPED -> ClientPlaybackTraceEventStage.AUDIO
            ClientPlaybackTraceEventEvent.FIRST_PICTURE_RENDERED,
            ClientPlaybackTraceEventEvent.FIRST_PICTURE_VISIBLE,
            ClientPlaybackTraceEventEvent.FRAME_DROP,
            ClientPlaybackTraceEventEvent.FRAME_LATE -> ClientPlaybackTraceEventStage.RENDER
        }

    private companion object {
        const val MAX_EVENTS = 128
        const val MAX_VALUE_MS = 300_000.0
        const val WINDOW_MS = 30_000L
        val ANOMALY_EVENTS = setOf(
            ClientPlaybackTraceEventEvent.TRANSPORT_GAP,
            ClientPlaybackTraceEventEvent.CONTINUITY_ERROR,
            ClientPlaybackTraceEventEvent.PTS_DISCONTINUITY,
            ClientPlaybackTraceEventEvent.PES_ERROR,
            ClientPlaybackTraceEventEvent.DECODE_ERROR,
            ClientPlaybackTraceEventEvent.DECODER_RECOVERY,
            ClientPlaybackTraceEventEvent.AUDIO_UNDERRUN,
            ClientPlaybackTraceEventEvent.FRAME_DROP,
            ClientPlaybackTraceEventEvent.FRAME_LATE
        )
    }
}
