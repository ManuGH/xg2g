package io.github.manugh.xg2g.android.playback.player

import android.media.MediaCodecList
import android.os.Build
import android.util.Log
import androidx.annotation.OptIn
import androidx.media3.common.Format
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.analytics.AnalyticsListener
import androidx.media3.exoplayer.source.LoadEventInfo
import androidx.media3.exoplayer.source.MediaLoadData
import io.github.manugh.xg2g.android.BuildConfig
import java.io.IOException

@OptIn(markerClass = [UnstableApi::class])
internal class Xg2gPlaybackLogger : AnalyticsListener {

    init {
        if (BuildConfig.DEBUG) {
            androidx.media3.common.util.Log.setLogLevel(androidx.media3.common.util.Log.LOG_LEVEL_ALL)
        }
        logDeviceHardwareSpecs()
    }

    override fun onLoadStarted(
        eventTime: AnalyticsListener.EventTime,
        loadEventInfo: LoadEventInfo,
        mediaLoadData: MediaLoadData,
        retryCount: Int
    ) {
        Log.i(
            TAG,
            "[XG2G_NETWORK] LoadStarted -> uri=${loadEventInfo.uri} dataType=${mediaLoadData.dataType} trackType=${mediaLoadData.trackType} startMs=${mediaLoadData.mediaStartTimeMs} endMs=${mediaLoadData.mediaEndTimeMs} retry=$retryCount"
        )
    }

    override fun onLoadCanceled(
        eventTime: AnalyticsListener.EventTime,
        loadEventInfo: LoadEventInfo,
        mediaLoadData: MediaLoadData
    ) {
        Log.w(
            TAG,
            "[XG2G_NETWORK] LoadCanceled -> uri=${loadEventInfo.uri} duration=${loadEventInfo.loadDurationMs}ms bytes=${loadEventInfo.bytesLoaded}"
        )
    }

    override fun onTracksChanged(
        eventTime: AnalyticsListener.EventTime,
        tracks: androidx.media3.common.Tracks
    ) {
        val groups = tracks.groups.joinToString(", ") { group ->
            (0 until group.length).joinToString(", ") { index ->
                val format = group.getTrackFormat(index)
                "${format.sampleMimeType}(sel=${group.isTrackSelected(index)},sup=${group.isTrackSupported(index)})"
            }
        }
        Log.i(TAG, "[XG2G_PLAYBACK] TracksChanged -> groups=${tracks.groups.size} [$groups]")
    }

    override fun onTimelineChanged(
        eventTime: AnalyticsListener.EventTime,
        reason: Int
    ) {
        val window = androidx.media3.common.Timeline.Window()
        val timeline = eventTime.timeline
        val info = if (timeline.windowCount > 0) {
            timeline.getWindow(0, window)
            "live=${window.isLive()} durationMs=${window.durationMs} defaultPosMs=${window.defaultPositionMs} dynamic=${window.isDynamic}"
        } else {
            "empty"
        }
        Log.i(TAG, "[XG2G_PLAYBACK] TimelineChanged -> reason=$reason $info")
    }

    override fun onDownstreamFormatChanged(
        eventTime: AnalyticsListener.EventTime,
        mediaLoadData: MediaLoadData
    ) {
        Log.i(
            TAG,
            "[XG2G_PLAYBACK] DownstreamFormat -> trackType=${mediaLoadData.trackType} format=${mediaLoadData.trackFormat?.sampleMimeType}"
        )
    }

    private fun logDeviceHardwareSpecs() {
        runCatching {
            Log.i(TAG, "[XG2G_DEVICE] Manufacturer=${Build.MANUFACTURER} Model=${Build.MODEL} Device=${Build.DEVICE} Hardware=${Build.HARDWARE} SDK=${Build.VERSION.SDK_INT}")
            val decoders = MediaCodecList(MediaCodecList.REGULAR_CODECS)
                .codecInfos
                .asSequence()
                .filterNot { it.isEncoder }
                .flatMap { info ->
                    info.supportedTypes.asSequence().map { type -> "${info.name} ($type)" }
                }
                .filter { it.contains("video/avc", ignoreCase = true) || it.contains("video/hevc", ignoreCase = true) }
                .take(10)
                .joinToString(", ")
            Log.i(TAG, "[XG2G_DEVICE] Video Decoders: $decoders")
        }.onFailure { err ->
            Log.w(TAG, "[XG2G_DEVICE] Failed to log device specs: ${err.message}")
        }
    }

    override fun onPlaybackStateChanged(
        eventTime: AnalyticsListener.EventTime,
        state: Int
    ) {
        val stateName = when (state) {
            Player.STATE_IDLE -> "IDLE"
            Player.STATE_BUFFERING -> "BUFFERING"
            Player.STATE_READY -> "READY"
            Player.STATE_ENDED -> "ENDED"
            else -> "UNKNOWN($state)"
        }
        Log.i(TAG, "[XG2G_PLAYBACK] PlaybackState -> $stateName (realtime=${eventTime.realtimeMs}ms)")
    }

    override fun onIsPlayingChanged(
        eventTime: AnalyticsListener.EventTime,
        isPlaying: Boolean
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] IsPlaying -> $isPlaying")
    }

    override fun onVideoSizeChanged(
        eventTime: AnalyticsListener.EventTime,
        videoSize: VideoSize
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] VideoSize -> ${videoSize.width}x${videoSize.height} (ratio=${videoSize.pixelWidthHeightRatio})")
    }

    override fun onSurfaceSizeChanged(
        eventTime: AnalyticsListener.EventTime,
        width: Int,
        height: Int
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] SurfaceSize -> ${width}x${height}")
    }

    override fun onVideoDecoderInitialized(
        eventTime: AnalyticsListener.EventTime,
        decoderName: String,
        initializedTimestampMs: Long,
        initializationDurationMs: Long
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] VideoDecoder -> $decoderName (initDuration=${initializationDurationMs}ms)")
    }

    override fun onAudioDecoderInitialized(
        eventTime: AnalyticsListener.EventTime,
        decoderName: String,
        initializedTimestampMs: Long,
        initializationDurationMs: Long
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] AudioDecoder -> $decoderName (initDuration=${initializationDurationMs}ms)")
    }

    override fun onDroppedVideoFrames(
        eventTime: AnalyticsListener.EventTime,
        droppedFrames: Int,
        elapsedMs: Long
    ) {
        Log.w(TAG, "[XG2G_PLAYBACK] DroppedVideoFrames -> dropped $droppedFrames frames in ${elapsedMs}ms")
    }

    override fun onPlayerError(
        eventTime: AnalyticsListener.EventTime,
        error: PlaybackException
    ) {
        Log.e(TAG, "[XG2G_PLAYBACK] PlayerError -> code=${error.errorCodeName}(${error.errorCode}) message=${error.message}", error)
    }

    override fun onLoadCompleted(
        eventTime: AnalyticsListener.EventTime,
        loadEventInfo: LoadEventInfo,
        mediaLoadData: MediaLoadData
    ) {
        val loadTimeMs = loadEventInfo.loadDurationMs
        val bytes = loadEventInfo.bytesLoaded
        Log.d(TAG, "[XG2G_NETWORK] LoadCompleted -> uri=${loadEventInfo.uri.lastPathSegment} duration=${loadTimeMs}ms bytes=$bytes")
    }

    override fun onLoadError(
        eventTime: AnalyticsListener.EventTime,
        loadEventInfo: LoadEventInfo,
        mediaLoadData: MediaLoadData,
        error: IOException,
        wasCanceled: Boolean
    ) {
        Log.e(TAG, "[XG2G_NETWORK] LoadError -> uri=${loadEventInfo.uri} duration=${loadEventInfo.loadDurationMs}ms canceled=$wasCanceled error=${error.message}", error)
    }

    override fun onVideoInputFormatChanged(
        eventTime: AnalyticsListener.EventTime,
        format: Format,
        decoderReuseEvaluation: androidx.media3.exoplayer.DecoderReuseEvaluation?
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] VideoInputFormat -> mime=${format.sampleMimeType} container=${format.containerMimeType} res=${format.width}x${format.height} fps=${format.frameRate} colorInfo=${format.colorInfo}")
    }

    override fun onAudioInputFormatChanged(
        eventTime: AnalyticsListener.EventTime,
        format: Format,
        decoderReuseEvaluation: androidx.media3.exoplayer.DecoderReuseEvaluation?
    ) {
        Log.i(TAG, "[XG2G_PLAYBACK] AudioInputFormat -> mime=${format.sampleMimeType} container=${format.containerMimeType} channels=${format.channelCount} sampleRate=${format.sampleRate}")
    }

    override fun onBandwidthEstimate(
        eventTime: AnalyticsListener.EventTime,
        totalLoadTimeMs: Int,
        totalBytesLoaded: Long,
        bitrateEstimate: Long
    ) {
        val mbps = bitrateEstimate / 1_000_000.0
        Log.i(TAG, "[XG2G_NETWORK] BandwidthEstimate -> ${"%.2f".format(mbps)} Mbps (totalBytes=$totalBytesLoaded)")
    }

    private companion object {
        const val TAG = "Xg2gPlayback"
    }
}
