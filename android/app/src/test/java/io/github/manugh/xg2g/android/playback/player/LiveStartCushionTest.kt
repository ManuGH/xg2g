package io.github.manugh.xg2g.android.playback.player

import androidx.media3.common.C
import androidx.media3.exoplayer.LoadControl
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.lang.reflect.Modifier

class LiveStartCushionTest {
    private val cushionUs = 6_000_000L
    private val liveTargetUs = 18_000_000L

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
