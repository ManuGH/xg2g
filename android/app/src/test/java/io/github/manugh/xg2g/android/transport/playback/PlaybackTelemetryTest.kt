// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package io.github.manugh.xg2g.android.transport.playback

import io.github.manugh.xg2g.android.contract.PlaybackTelemetryBatch
import io.github.manugh.xg2g.android.contract.PlaybackTelemetryEventKind
import java.time.Instant
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PlaybackTelemetryTest {

    private fun healthy(step: Long): PlaybackTelemetrySnapshot = PlaybackTelemetrySnapshot(
        bytesReceivedTotal = 1_000_000L * step,
        decodedFramesTotal = 375L * step,
        presentedFramesTotal = 750L * step,
        audioUnderrunsTotal = 0L,
        networkStallsTotal = 0L,
        decodeErrorsTotal = 0L,
        decoderRecoveriesTotal = 0L,
        droppedFramesTotal = 0L,
        warnings = emptyList(),
        ttfpMs = 250.0
    )

    @Test
    fun healthyWindowIsAHeartbeat() {
        val window = PlaybackTelemetryWindow.evaluate(
            previous = healthy(1),
            current = healthy(2),
            windowSeconds = 15.0
        )
        assertTrue(window.reasons.isEmpty())
        assertFalse(window.isDegraded)
        assertEquals(750.0, window.metrics["presentedFieldsDelta"]!!, 0.001)
        assertEquals(15.0, window.metrics["windowSeconds"]!!, 0.001)
    }

    @Test
    fun frozenPictureIsCaughtDespiteBytesFlowing() {
        val prev = healthy(1)
        val current = healthy(2).copy(presentedFramesTotal = prev.presentedFramesTotal)

        val window = PlaybackTelemetryWindow.evaluate(previous = prev, current = current, windowSeconds = 15.0)
        assertEquals(listOf("presentation_stalled"), window.reasons)
        assertTrue(window.isDegraded)
    }

    @Test
    fun decoderThatStopsIsNamed() {
        val prev = healthy(1)
        val current = healthy(2).copy(
            decodedFramesTotal = prev.decodedFramesTotal,
            presentedFramesTotal = prev.presentedFramesTotal
        )

        val window = PlaybackTelemetryWindow.evaluate(previous = prev, current = current, windowSeconds = 15.0)
        assertEquals(listOf("video_decode_stalled"), window.reasons)
    }

    @Test
    fun streamThatStopsArrivingIsNoInput() {
        val prev = healthy(1)
        val current = healthy(1)

        val window = PlaybackTelemetryWindow.evaluate(previous = prev, current = current, windowSeconds = 15.0)
        assertEquals(listOf("no_input"), window.reasons)
    }

    @Test
    fun firstWindowIsNeverAStall() {
        val window = PlaybackTelemetryWindow.evaluate(
            previous = null,
            current = PlaybackTelemetrySnapshot(),
            windowSeconds = 15.0
        )
        assertTrue(window.reasons.isEmpty())
        assertFalse(window.isDegraded)
    }

    @Test
    fun audioStarvationIsNamedAboveThreshold() {
        val prev = healthy(1)
        val underThreshold = healthy(2).copy(
            audioUnderrunsTotal = PlaybackTelemetryWindow.AUDIO_UNDERRUN_THRESHOLD.toLong() - 1
        )
        val windowClean = PlaybackTelemetryWindow.evaluate(previous = prev, current = underThreshold, windowSeconds = 15.0)
        assertTrue(windowClean.reasons.isEmpty())

        val atThreshold = healthy(2).copy(
            audioUnderrunsTotal = PlaybackTelemetryWindow.AUDIO_UNDERRUN_THRESHOLD.toLong()
        )
        val windowDegraded = PlaybackTelemetryWindow.evaluate(previous = prev, current = atThreshold, windowSeconds = 15.0)
        assertEquals(listOf("audio_underruns"), windowDegraded.reasons)
    }

    @Test
    fun networkStallsDecodeErrorsAndWarningsAreEachNamed() {
        val prev = healthy(1)
        val current = healthy(2).copy(
            networkStallsTotal = 1L,
            decodeErrorsTotal = 3L,
            warnings = listOf("decoder recovery: error in OMX.MTK")
        )

        val window = PlaybackTelemetryWindow.evaluate(previous = prev, current = current, windowSeconds = 15.0)
        assertEquals(listOf("network_stalls", "decode_errors", "pipeline_warning"), window.reasons)
        assertEquals("decoder recovery: error in OMX.MTK", window.detail)
    }

    @Test
    fun resetCounterCountsFromZero() {
        val prev = healthy(1).copy(audioUnderrunsTotal = 400L)
        val current = healthy(2).copy(audioUnderrunsTotal = 12L)

        val window = PlaybackTelemetryWindow.evaluate(previous = prev, current = current, windowSeconds = 15.0)
        assertEquals(12.0, window.metrics["audioUnderrunsDelta"]!!, 0.001)
        assertEquals(listOf("audio_underruns"), window.reasons)
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    @Test
    fun reporterEmitsLifecycleEventsInOrder() = runTest {
        val testDispatcher = StandardTestDispatcher(testScheduler)
        val testScope = TestScope(testDispatcher)

        val sentBatches = mutableListOf<PlaybackTelemetryBatch>()
        val fakeSink = object : PlaybackTelemetrySink {
            override suspend fun send(batch: PlaybackTelemetryBatch) {
                sentBatches.add(batch)
            }
        }

        var currentSnapshot = healthy(1)
        var currentTime = Instant.parse("2026-09-27T10:00:00Z")

        val reporter = PlaybackTelemetryReporter(
            scope = testScope,
            sinkProvider = { fakeSink },
            snapshotProvider = { currentSnapshot },
            mainDispatcher = testDispatcher,
            ioDispatcher = testDispatcher,
            now = { currentTime }
        )

        // 1. Watch -> session_start
        reporter.watch(
            zapId = "zap-123",
            serviceRef = "1:0:19:283D:3FB:1:C00000:0:0:0:",
            startMetrics = mapOf("ttfpTotalMs" to 120.0)
        )
        testScheduler.runCurrent()

        assertEquals(1, sentBatches.size)
        val startEvent = sentBatches[0].events[0]
        assertEquals(PlaybackTelemetryEventKind.SESSION_START, startEvent.kind)
        assertEquals("zap-123", startEvent.zapId)
        assertEquals(120.0, startEvent.metrics?.get("ttfpTotalMs")!!, 0.001)

        // 2. Advance time and sample -> heartbeat
        currentTime = currentTime.plusSeconds(15)
        currentSnapshot = healthy(2)
        reporter.sample()
        testScheduler.runCurrent()

        assertEquals(2, sentBatches.size)
        val heartbeatEvent = sentBatches[1].events[0]
        assertEquals(PlaybackTelemetryEventKind.HEARTBEAT, heartbeatEvent.kind)
        assertNull(heartbeatEvent.reasons)

        // 3. Finish -> session_end
        currentTime = currentTime.plusSeconds(5)
        currentSnapshot = healthy(3)
        reporter.finish("channel change")
        testScheduler.runCurrent()

        assertEquals(3, sentBatches.size)
        val endEvent = sentBatches[2].events[0]
        assertEquals(PlaybackTelemetryEventKind.SESSION_END, endEvent.kind)
        assertEquals("channel change", endEvent.detail)
    }
}
