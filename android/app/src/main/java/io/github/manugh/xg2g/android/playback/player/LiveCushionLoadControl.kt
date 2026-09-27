package io.github.manugh.xg2g.android.playback.player

import androidx.annotation.OptIn
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.DefaultLoadControl
import androidx.media3.exoplayer.LoadControl

/**
 * Live playback runs at a pinned 1.0x speed (AC-3 passthrough cannot follow a speed change), so
 * the distance to the live edge is fixed the moment playback starts: Media3 can never slow down
 * to rebuild it. A freshly started session's window is only a segment or two long, which lands
 * the player on the live edge, and the stock rule (start after min(bufferForPlaybackMs,
 * targetOffset / 2), here 1 s) then stalls within a segment; each later stall resumes after only
 * 3.5 s and stalls again. Segment cadence, playlist reload and download together need about
 * [LIVE_CUSHION_US] of media in hand, and waiting is the only way to get it, so live playback
 * starts, and resumes after a stall, only once that much is buffered. Recordings keep the
 * delegate's fast-start thresholds.
 */
@OptIn(markerClass = [UnstableApi::class])
internal class LiveCushionLoadControl(
    private val delegate: DefaultLoadControl,
    private val bufferFull: () -> Boolean,
    private val liveCushionUs: Long = LIVE_CUSHION_US
) : LoadControl by delegate {

    override fun shouldStartPlayback(parameters: LoadControl.Parameters): Boolean =
        liveStartGate(
            targetLiveOffsetUs = parameters.targetLiveOffsetUs,
            bufferedDurationUs = parameters.bufferedDurationUs,
            playbackSpeed = parameters.playbackSpeed,
            liveCushionUs = liveCushionUs,
            bufferFull = bufferFull()
        ) ?: delegate.shouldStartPlayback(parameters)

    companion object {
        const val LIVE_CUSHION_US = 6_000_000L
    }
}

/**
 * Returns null for non-live media, where the caller applies the stock rule. Live media may start
 * once the buffer covers the cushion, capped at the target live offset so a low-latency target
 * never waits for more than itself, or once the byte budget is exhausted, because loading stops
 * there and waiting longer would never end.
 */
internal fun liveStartGate(
    targetLiveOffsetUs: Long,
    bufferedDurationUs: Long,
    playbackSpeed: Float,
    liveCushionUs: Long,
    bufferFull: Boolean
): Boolean? {
    if (targetLiveOffsetUs == C.TIME_UNSET) {
        return null
    }
    if (bufferFull) {
        return true
    }
    val playoutUs = if (playbackSpeed == 1f) {
        bufferedDurationUs
    } else {
        (bufferedDurationUs / playbackSpeed).toLong()
    }
    return playoutUs >= minOf(liveCushionUs, targetLiveOffsetUs)
}
