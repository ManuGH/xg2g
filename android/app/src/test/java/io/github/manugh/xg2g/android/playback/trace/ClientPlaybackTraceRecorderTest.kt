package io.github.manugh.xg2g.android.playback.trace

import io.github.manugh.xg2g.android.contract.ClientPlaybackTraceEventEvent
import io.github.manugh.xg2g.android.contract.ClientPlaybackTraceEventStage
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ClientPlaybackTraceRecorderTest {
    @Test
    fun recordsOrderedEventsAndTriggersOnlyForAnomalies() {
        var elapsedMs = 1_000L
        val recorder = ClientPlaybackTraceRecorder(
            elapsedRealtimeMs = { elapsedMs },
            wallClock = { Instant.parse("2026-09-27T10:00:00Z") }
        )

        assertNull(recorder.snapshot())
        recorder.startPlayback()
        elapsedMs += 250
        assertFalse(recorder.record(ClientPlaybackTraceEventEvent.REQUEST_STARTED))
        elapsedMs += 900
        assertTrue(recorder.record(ClientPlaybackTraceEventEvent.FRAME_DROP, 900.0))
        assertTrue(recorder.hasAnomalyAfter(2L))
        assertFalse(recorder.hasAnomalyAfter(3L))

        val batch = requireNotNull(recorder.snapshot())
        assertEquals(Instant.parse("2026-09-27T10:00:00Z"), batch.observedAt)
        assertEquals(listOf(1L, 2L, 3L), batch.events.map { it.sequence })
        assertEquals(listOf(0L, 250L, 1_150L), batch.events.map { it.elapsedMs })
        assertEquals(ClientPlaybackTraceEventStage.RENDER, batch.events.last().stage)
        assertEquals(900.0, batch.events.last().valueMs)
    }

    @Test
    fun snapshotRetainsOnlyTheRecentBoundedWindow() {
        var elapsedMs = 0L
        val recorder = ClientPlaybackTraceRecorder(
            elapsedRealtimeMs = { elapsedMs },
            maxEvents = 4,
            windowMs = 30_000
        )
        recorder.startPlayback()
        repeat(5) {
            elapsedMs += 1_000
            recorder.record(ClientPlaybackTraceEventEvent.REQUEST_STARTED)
        }

        var batch = requireNotNull(recorder.snapshot())
        assertEquals(4, batch.events.size)
        assertEquals(3L, batch.events.first().sequence)

        elapsedMs += 31_000
        recorder.record(ClientPlaybackTraceEventEvent.REQUEST_STARTED)
        batch = requireNotNull(recorder.snapshot())
        assertEquals(1, batch.events.size)
        assertEquals(7L, batch.events.single().sequence)
    }

    @Test
    fun endingPlaybackDiscardsItsTraceWindow() {
        var elapsedMs = 100L
        val recorder = ClientPlaybackTraceRecorder(elapsedRealtimeMs = { elapsedMs })
        recorder.startPlayback()
        recorder.record(ClientPlaybackTraceEventEvent.DECODE_ERROR)
        recorder.endPlayback()

        elapsedMs += 100
        assertNull(recorder.snapshot())
        assertFalse(recorder.record(ClientPlaybackTraceEventEvent.FRAME_DROP))
    }

    @Test
    fun ignoresNonFiniteAndOutOfRangeMeasurements() {
        val recorder = ClientPlaybackTraceRecorder(elapsedRealtimeMs = { 100L })
        recorder.startPlayback()
        recorder.record(ClientPlaybackTraceEventEvent.FRAME_DROP, Double.POSITIVE_INFINITY)
        recorder.record(ClientPlaybackTraceEventEvent.FRAME_LATE, 300_001.0)

        val batch = requireNotNull(recorder.snapshot())
        assertEquals(listOf(null, null), batch.events.drop(1).map { it.valueMs })
    }
}
