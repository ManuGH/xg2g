package io.github.manugh.xg2g.android.playback.player

import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.LoadControl
import androidx.media3.exoplayer.analytics.PlayerId
import androidx.media3.exoplayer.source.MediaSource
import androidx.media3.exoplayer.source.SinglePeriodTimeline
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.lang.reflect.Modifier

class LiveStartCushionTest {
    private val cushionUs = 6_000_000L
    private val liveTargetUs = 18_000_000L

    private fun stockControl() = DefaultLoadControl.Builder()
        .setBufferDurationsMs(15_000, 30_000, 1_000, 3_500)
        .setTargetBufferBytes(20 * 1024 * 1024)
        .setPrioritizeTimeOverSizeThresholds(false)
        .build()

    private fun parameters(bufferedUs: Long, liveOffsetUs: Long, rebuffering: Boolean): LoadControl.Parameters {
        val timeline = SinglePeriodTimeline(60_000_000L, true, true, true, null, MediaItem.EMPTY)
        return LoadControl.Parameters(
            PlayerId.UNSET, timeline, MediaSource.MediaPeriodId(timeline.getUidOfPeriod(0)),
            0L, bufferedUs, 1f, true, rebuffering, liveOffsetUs, C.TIME_UNSET
        )
    }

    @Test
    fun `actual Media3 cold start and rebuffer retain the live cushion through stop and prepare`() {
        val stock = stockControl()
        val wrapped = LiveCushionLoadControl(stockControl(), bufferFull = { false })
        stock.onPrepared(PlayerId.UNSET)
        wrapped.onPrepared(PlayerId.UNSET)
        for ((bufferedUs, rebuffering) in listOf(1_280_000L to false, 3_500_000L to true)) {
            val p = parameters(bufferedUs, liveTargetUs, rebuffering)
            assertEquals(true, stock.shouldStartPlayback(p))
            assertEquals(false, wrapped.shouldStartPlayback(p))
            assertEquals(true, wrapped.shouldContinueLoading(p))
        }
        assertEquals(true, wrapped.shouldStartPlayback(parameters(cushionUs, liveTargetUs, false)))
        assertEquals(true, wrapped.shouldStartPlayback(parameters(cushionUs, liveTargetUs, true)))
        wrapped.onStopped(PlayerId.UNSET)
        wrapped.onReleased(PlayerId.UNSET)
        wrapped.onPrepared(PlayerId.UNSET)
        assertEquals(false, wrapped.shouldStartPlayback(parameters(1_280_000L, liveTargetUs, false)))
        assertEquals(true, wrapped.shouldStartPlayback(parameters(1_280_000L, C.TIME_UNSET, false)))
        wrapped.getAllocator(PlayerId.UNSET)
        wrapped.getBackBufferDurationUs(PlayerId.UNSET)
        wrapped.retainBackBufferFromKeyframe(PlayerId.UNSET)
        wrapped.onReleased(PlayerId.UNSET)
        stock.onReleased(PlayerId.UNSET)
    }

    @Test
    fun `live cushion measures playout duration rather than media duration at higher speed`() {
        assertEquals(false, liveStartGate(liveTargetUs, cushionUs, 2f, cushionUs, bufferFull = false))
        assertEquals(true, liveStartGate(liveTargetUs, 12_000_000L, 2f, cushionUs, bufferFull = false))
    }

    @Test
    fun `recordings defer to the stock thresholds`() {
        assertNull(liveStartGate(C.TIME_UNSET, 500_000L, 1f, cushionUs, bufferFull = false))
    }

    @Test
    fun `live does not start on a fresh session's first short segment`() {
        assertEquals(false, liveStartGate(liveTargetUs, 1_280_000L, 1f, cushionUs, bufferFull = false))
    }

    @Test
    fun `live does not resume after a stall with only the old 3,5 s`() {
        assertEquals(false, liveStartGate(liveTargetUs, 3_500_000L, 1f, cushionUs, bufferFull = false))
    }

    @Test
    fun `live starts once the cushion is buffered`() {
        assertEquals(true, liveStartGate(liveTargetUs, cushionUs, 1f, cushionUs, bufferFull = false))
    }

    @Test
    fun `a lower live target caps the cushion`() {
        assertEquals(true, liveStartGate(4_000_000L, 4_000_000L, 1f, cushionUs, bufferFull = false))
    }

    @Test
    fun `an exhausted byte budget starts playback instead of deadlocking`() {
        assertEquals(true, liveStartGate(liveTargetUs, 2_000_000L, 1f, cushionUs, bufferFull = true))
    }

    /**
     * Kotlin's `by` delegation does not forward Java default methods. Every LoadControl method
     * Media3 calls is a default that throws "not implemented", so a delegating wrapper crashes
     * when ExoPlayer is built. Each current method must be forwarded explicitly.
     */
    @Test
    fun `every current LoadControl method is forwarded explicitly`() {
        val missing = LoadControl::class.java.methods
            .filter { !Modifier.isStatic(it.modifiers) }
            .filter { !it.isAnnotationPresent(java.lang.Deprecated::class.java) }
            .filter { method ->
                runCatching {
                    LiveCushionLoadControl::class.java.getDeclaredMethod(method.name, *method.parameterTypes)
                }.isFailure
            }
            .map { method -> "${method.name}(${method.parameterTypes.joinToString { it.simpleName }})" }
            .sorted()
        assertEquals(emptyList<String>(), missing)
    }
}
