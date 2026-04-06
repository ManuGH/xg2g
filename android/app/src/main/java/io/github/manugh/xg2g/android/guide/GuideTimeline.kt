package io.github.manugh.xg2g.android.guide

internal data class GuideTimelineWindow(
    val startEpochSec: Long,
    val endEpochSec: Long
) {
    val durationSeconds: Long
        get() = (endEpochSec - startEpochSec).coerceAtLeast(0L)
}

internal fun buildGuideTimelineWindow(
    nowEpochSec: Long,
    slotSeconds: Long = 30 * 60,
    windowSeconds: Long = 3 * 60 * 60
): GuideTimelineWindow {
    val alignedStart = nowEpochSec - (nowEpochSec % slotSeconds)
    return GuideTimelineWindow(
        startEpochSec = alignedStart,
        endEpochSec = alignedStart + windowSeconds
    )
}

internal fun canonicalGuideServiceRef(ref: String): String {
    val trimmed = ref.trim().trimEnd(':')
    if (trimmed.isEmpty()) {
        return ""
    }
    return if (trimmed.all { it == ':' || it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }) {
        trimmed.uppercase()
    } else {
        trimmed
    }
}

internal fun deriveGuideNowNext(
    schedule: List<GuideProgram>,
    currentEpochSec: Long
): Pair<GuideProgram?, GuideProgram?> {
    val ordered = schedule.sortedBy(GuideProgram::startEpochSec)
    val now = ordered.firstOrNull {
        currentEpochSec >= it.startEpochSec && currentEpochSec < it.endEpochSec
    }
    val next = ordered.firstOrNull { it.startEpochSec >= currentEpochSec }
    return now to next
}

internal fun GuideProgram.overlaps(window: GuideTimelineWindow): Boolean =
    endEpochSec > window.startEpochSec && startEpochSec < window.endEpochSec

internal fun GuideProgram.visibleDurationSeconds(window: GuideTimelineWindow): Long =
    (minOf(endEpochSec, window.endEpochSec) - maxOf(startEpochSec, window.startEpochSec)).coerceAtLeast(0L)
