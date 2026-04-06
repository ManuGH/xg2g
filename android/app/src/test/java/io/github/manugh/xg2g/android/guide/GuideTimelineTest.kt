package io.github.manugh.xg2g.android.guide

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class GuideTimelineTest {

    @Test
    fun `canonicalGuideServiceRef normalizes hex refs and trims trailing colon`() {
        assertEquals(
            "1:0:19:EF75:421:1:C00000:0:0:0",
            canonicalGuideServiceRef(" 1:0:19:ef75:421:1:c00000:0:0:0: ")
        )
    }

    @Test
    fun `buildGuideTimelineWindow aligns start to previous half hour`() {
        val window = buildGuideTimelineWindow(nowEpochSec = 1_710_000_901L)

        assertEquals(1_710_000_000L, window.startEpochSec)
        assertEquals(10_800L, window.durationSeconds)
        assertEquals(1_710_010_800L, window.endEpochSec)
    }

    @Test
    fun `deriveGuideNowNext picks current and following programme`() {
        val schedule = listOf(
            GuideProgram("Earlier", 1_000L, 1_200L),
            GuideProgram("Current", 1_200L, 1_800L),
            GuideProgram("Next", 1_800L, 2_100L)
        )

        val (now, next) = deriveGuideNowNext(schedule, currentEpochSec = 1_500L)

        assertEquals("Current", now?.title)
        assertEquals("Next", next?.title)
    }

    @Test
    fun `deriveGuideNowNext returns no next when window ends after current`() {
        val schedule = listOf(
            GuideProgram("Current", 1_200L, 1_800L)
        )

        val (now, next) = deriveGuideNowNext(schedule, currentEpochSec = 1_500L)

        assertEquals("Current", now?.title)
        assertNull(next)
    }

    @Test
    fun `visibleDurationSeconds clips programme to timeline window`() {
        val window = GuideTimelineWindow(startEpochSec = 1_200L, endEpochSec = 1_800L)
        val program = GuideProgram("Film", startEpochSec = 1_000L, endEpochSec = 1_500L)

        assertEquals(300L, program.visibleDurationSeconds(window))
    }
}
