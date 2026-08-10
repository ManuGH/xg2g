package io.github.manugh.xg2g.android.playback.player

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Log
import androidx.annotation.OptIn
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.common.MimeTypes
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlaybackException
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.mediacodec.MediaCodecSelector
import androidx.media3.exoplayer.mediacodec.MediaCodecUtil
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.datasource.okhttp.OkHttpDataSource
import okhttp3.OkHttpClient

@OptIn(markerClass = [UnstableApi::class])
internal class PlayerHolder(
    context: Context,
    private val okHttpClient: OkHttpClient
) {
    private companion object {
        const val TAG = "PlayerHolder"
        // Sitting only three segments behind the live edge leaves no headroom: any hiccup drops
        // the player into a rebuffer, and a rebuffer is what most reliably kills the MediaTek
        // decoder (google/ExoPlayer#10285). A wider offset costs latency, never a frame.
        const val LIVE_TARGET_OFFSET_MS = 18_000L
        const val LIVE_MIN_OFFSET_MS = 10_000L
        const val LIVE_MAX_OFFSET_MS = 40_000L

        /**
         * The MediaTek decoder fails in bursts — two failures seconds apart are normal — so the
         * budget only exists to stop a genuinely hopeless stream from looping forever.
         */
        const val MAX_RECOVERIES_PER_WINDOW = 8
        const val RECOVERY_WINDOW_MS = 300_000L

        /** Grace period between rebuilding the player and preparing the stream on it. */
        const val REARM_DELAY_MS = 500L

        /**
         * A rebuild is only worth doing for transient faults. If playback dies again within this
         * window after being re-armed, the fault is deterministic — the ATV HD MP2 audio decoder
         * failed instantly on every single rebuild, so the old unconditional loop rebuilt 60 times
         * in 70 seconds and then left the stream dead without telling anyone.
         */
        const val MIN_HEALTHY_PLAYBACK_MS = 5_000L
        const val MAX_FAST_FAILURES = 3
        const val MAX_BACKOFF_MS = 8_000L

        /** Rendered-frame watchdog: no new frames for [MAX_STALLED_CHECKS] ticks means stalled. */
        const val WATCHDOG_INTERVAL_MS = 2_000L
        const val MAX_STALLED_CHECKS = 3

        /** Hard ceiling on buffered media (~16s of this 10Mbps broadcast) to stay off the LMK. */
        const val MAX_BUFFER_BYTES = 20 * 1024 * 1024
    }

    private val appContext = context.applicationContext
    private val handler = Handler(Looper.getMainLooper())

    /** Parameters of the stream currently loaded, replayed verbatim when the player is rebuilt. */
    private data class PlaybackRequest(
        val url: String,
        val mediaId: String,
        val title: String?,
        val isLive: Boolean,
        val requestHeaders: Map<String, String>,
        val mimeType: String?,
        val startPositionMs: Long
    )

    private var lastRequest: PlaybackRequest? = null
    private var recoveryCount = 0
    private var recoveryWindowStartMs = 0L
    private var isRecovering = false
    private var terminalReason: String? = null
    private var requestGeneration = 0L
    private var audioDisabled = false
    private var watchdogEnabled = false

    /**
     * Invoked on the main thread after the ExoPlayer instance has been replaced. Everything
     * holding a reference (PlayerView, MediaSession, event forwarder) must re-attach.
     */
    var onPlayerReplaced: ((ExoPlayer) -> Unit)? = null

    /**
     * Invoked when rebuilding has been abandoned, so the UI can show a real error instead of a
     * frozen frame. The argument is a short human-readable reason.
     */
    var onUnrecoverable: ((String) -> Unit)? = null

    /** Invoked when playback can continue with a reduced feature set, currently video-only. */
    var onDegraded: ((String) -> Unit)? = null

    private var lastRearmAtMs = 0L
    private var consecutiveFastFailures = 0
    private val playbackLogger = Xg2gPlaybackLogger()

    /**
     * media3 ranks the Fire TV software AVC decoder (c2.android.avc.decoder) ahead of the
     * MediaTek hardware decoder because the latter under-reports format support — a known
     * ExoPlayer regression since 2.11 (google/ExoPlayer#9565, amzn/exoplayer-amazon-port#115).
     * The software decoder only manages 30fps at 720p, so 50fps broadcasts lose every second
     * frame. Hardware-accelerated decoders go first; decoder fallback still covers failures.
     */
    private fun createCodecSelector() = MediaCodecSelector { mimeType, requiresSecureDecoder, requiresTunnelingDecoder ->
        val infos = MediaCodecUtil.getDecoderInfos(mimeType, requiresSecureDecoder, requiresTunnelingDecoder)
        if (mimeType.startsWith("video/")) {
            infos.sortedBy { info -> if (info.hardwareAccelerated) 0 else 1 }
        } else {
            infos
        }
    }

    /**
     * Shared across every rebuilt player. The Fire TV Stick has ~1.7GB of RAM and already runs
     * close to the low-memory killer, so handing each new ExoPlayer its own pool — media3
     * defaults to targeting 50s of buffered media — churns memory until the system kills the
     * app mid-stream. One allocator, reused, with an explicit byte ceiling.
     */
    private val sharedAllocator = androidx.media3.exoplayer.upstream.DefaultAllocator(
        true,
        C.DEFAULT_BUFFER_SEGMENT_SIZE
    )

    var player: ExoPlayer = createPlayer()
        private set

    private fun createLoadControl() = androidx.media3.exoplayer.DefaultLoadControl.Builder()
        .setAllocator(sharedAllocator)
        .setBufferDurationsMs(
            /* minBufferMs = */ 15_000,
            /* maxBufferMs = */ 30_000,
            // Copy streams can begin with a short partial-GOP segment (~640ms).
            // One second keeps the fast path fast but prevents Media3 from
            // consuming that fragment before the first full segment is published.
            /* bufferForPlaybackMs = */ 1_000,
            /* bufferForPlaybackAfterRebufferMs = */ 3_500
        )
        .setTargetBufferBytes(MAX_BUFFER_BYTES)
        .setPrioritizeTimeOverSizeThresholds(false)
        .build()

    private fun createPlayer(): ExoPlayer {
        val renderersFactory = Xg2gRenderersFactory(appContext)
            .setEnableAudioOutputPlaybackParameters(false)
            .setEnableDecoderFallback(true)
            .setMediaCodecSelector(createCodecSelector())

        return ExoPlayer.Builder(appContext, renderersFactory)
            .setLoadControl(createLoadControl())
            .build()
            .apply {
                addAnalyticsListener(playbackLogger)
                addListener(object : Player.Listener {
                    override fun onPlayerError(error: PlaybackException) {
                        val exoError = error as? ExoPlaybackException
                        if (exoError?.type != ExoPlaybackException.TYPE_RENDERER) {
                            return
                        }
                        when (rendererTrackType(exoError, this@apply)) {
                            C.TRACK_TYPE_VIDEO -> rebuildPlayer(error.errorCodeName, exoError.rendererName)
                            C.TRACK_TYPE_AUDIO -> degradeAudio(error.errorCodeName, exoError.rendererName)
                        }
                    }

                    override fun onRenderedFirstFrame() {
                        scheduleHealthyPlaybackReset()
                    }
                })
                setAudioAttributes(
                    AudioAttributes.Builder()
                        .setUsage(C.USAGE_MEDIA)
                        .setContentType(C.AUDIO_CONTENT_TYPE_MOVIE)
                        .build(),
                    true
                )
                if (audioDisabled) {
                    disableTrackType(C.TRACK_TYPE_AUDIO)
                }
                playWhenReady = true
            }
    }

    private fun rendererTrackType(error: ExoPlaybackException, sourcePlayer: ExoPlayer): Int {
        if (error.rendererIndex in 0 until sourcePlayer.rendererCount) {
            return sourcePlayer.getRendererType(error.rendererIndex)
        }
        return MimeTypes.getTrackType(error.rendererFormat?.sampleMimeType)
    }

    private fun ExoPlayer.disableTrackType(trackType: Int) {
        trackSelectionParameters = trackSelectionParameters
            .buildUpon()
            .setTrackTypeDisabled(trackType, true)
            .build()
    }

    private fun ExoPlayer.enableTrackType(trackType: Int) {
        trackSelectionParameters = trackSelectionParameters
            .buildUpon()
            .setTrackTypeDisabled(trackType, false)
            .build()
    }

    private val healthyPlaybackReset = Runnable {
        if (terminalReason == null && !isRecovering && player.isPlaying) {
            consecutiveFastFailures = 0
            Log.i(TAG, "[DECODER_RECOVERY] playback remained healthy; recovery streak cleared")
        }
    }

    private fun scheduleHealthyPlaybackReset() {
        handler.removeCallbacks(healthyPlaybackReset)
        handler.postDelayed(healthyPlaybackReset, MIN_HEALTHY_PLAYBACK_MS)
    }

    /**
     * The MediaTek AVC decoder on Fire TV fails mid-stream and cannot be flushed afterwards:
     * `releaseOutputBuffer` throws, which leaves the codec unable to recover, so re-preparing
     * the same ExoPlayer decodes into a surface that never updates again — the picture freezes
     * while audio keeps running (google/ExoPlayer#11222, amzn/exoplayer-amazon-port#81).
     * Only a full rebuild of the player, with a fresh codec and a fresh surface attachment,
     * restores video.
     */
    /**
     * The same decoder also fails silently: it keeps accepting input and the player stays in
     * STATE_READY with audio running, but stops rendering frames entirely (androidx/media#2711).
     * Nothing is thrown, so only a watchdog on the rendered-frame counter notices it.
     */
    private val stallWatchdog = object : Runnable {
        override fun run() {
            if (!watchdogEnabled || terminalReason != null || lastRequest == null) {
                return
            }
            checkVideoProgress()
            if (watchdogEnabled && terminalReason == null && lastRequest != null) {
                handler.postDelayed(this, WATCHDOG_INTERVAL_MS)
            }
        }
    }

    private var lastRenderedFrames = -1
    private var lastPlaybackPositionMs = C.TIME_UNSET
    private var stalledChecks = 0

    private fun checkVideoProgress() {
        if (isRecovering || terminalReason != null) {
            return
        }
        val current = player
        if (
            !current.isPlaying ||
            current.playbackState != Player.STATE_READY ||
            current.videoFormat == null ||
            android.os.SystemClock.elapsedRealtime() - lastRearmAtMs < MIN_HEALTHY_PLAYBACK_MS
        ) {
            resetWatchdogSample()
            return
        }
        val counters = current.videoDecoderCounters ?: return
        counters.ensureUpdated()
        val rendered = counters.renderedOutputBufferCount
        val positionMs = current.currentPosition
        if (rendered != lastRenderedFrames) {
            lastRenderedFrames = rendered
            lastPlaybackPositionMs = positionMs
            stalledChecks = 0
            return
        }
        val playbackClockAdvanced = lastPlaybackPositionMs != C.TIME_UNSET && positionMs > lastPlaybackPositionMs + 250L
        lastPlaybackPositionMs = positionMs
        if (!playbackClockAdvanced) {
            stalledChecks = 0
            return
        }
        stalledChecks += 1
        if (stalledChecks >= MAX_STALLED_CHECKS) {
            Log.w(TAG, "[DECODER_RECOVERY] video output stalled at $rendered rendered frames while playing")
            rebuildPlayer("VIDEO_OUTPUT_STALLED", renderer = null)
        }
    }

    private fun resetWatchdogSample() {
        lastRenderedFrames = -1
        lastPlaybackPositionMs = C.TIME_UNSET
        stalledChecks = 0
    }

    private fun rebuildPlayer(reason: String, renderer: String?) {
        if (isRecovering || terminalReason != null) {
            return
        }
        val request = lastRequest
        if (request == null) {
            Log.w(TAG, "[DECODER_RECOVERY] $reason but no stream to replay -> giving up")
            return
        }

        handler.removeCallbacks(healthyPlaybackReset)
        val label = renderer?.let { "$reason in $it" } ?: reason
        val sinceRearm = android.os.SystemClock.elapsedRealtime() - lastRearmAtMs
        if (lastRearmAtMs != 0L && sinceRearm < MIN_HEALTHY_PLAYBACK_MS) {
            consecutiveFastFailures++
        } else {
            consecutiveFastFailures = 0
        }

        // Rebuilding only helps a decoder that was working and fell over. A fault that reappears
        // immediately on a freshly built player will reappear on the next one too.
        if (consecutiveFastFailures >= MAX_FAST_FAILURES) {
            Log.e(TAG, "[DECODER_RECOVERY] $label failed $consecutiveFastFailures times within ${MIN_HEALTHY_PLAYBACK_MS}ms of each rebuild -> deterministic, stopping")
            giveUp(label)
            return
        }
        if (!allowRecovery()) {
            Log.e(TAG, "[DECODER_RECOVERY] $MAX_RECOVERIES_PER_WINDOW failures within ${RECOVERY_WINDOW_MS}ms -> not rebuilding again")
            giveUp(label)
            return
        }

        isRecovering = true
        resetWatchdogSample()
        val backoffMs = (REARM_DELAY_MS shl consecutiveFastFailures).coerceAtMost(MAX_BACKOFF_MS)
        val recoveryGeneration = requestGeneration
        Log.w(TAG, "[DECODER_RECOVERY] $label -> rebuilding player (attempt $recoveryCount, fastFailures=$consecutiveFastFailures, backoff=${backoffMs}ms)")

        // Never release a player from inside its own callback.
        handler.post {
            if (requestGeneration != recoveryGeneration || lastRequest == null) {
                isRecovering = false
                return@post
            }
            val previous = player
            runCatching { previous.release() }
                .onFailure { err -> Log.w(TAG, "[DECODER_RECOVERY] releasing old player failed: ${err.message}") }

            player = createPlayer()
            onPlayerReplaced?.invoke(player)

            // Re-arming is deliberately deferred: the crashed MediaTek OMX component needs a
            // moment to tear down, and the PlayerView must bind its surface to the new player
            // first. Preparing earlier makes the codec start against a placeholder surface and
            // the real one is then handed over via MediaCodec.setOutputSurface, which is broken
            // on these devices — the decoder runs but the picture never updates again.
            handler.postDelayed({
                if (requestGeneration != recoveryGeneration || lastRequest == null) {
                    // Playback was stopped or replaced while the rebuild was pending.
                    isRecovering = false
                    return@postDelayed
                }
                // Live streams resume at the live edge rather than where the decoder died.
                playUrl(
                    url = request.url,
                    mediaId = request.mediaId,
                    title = request.title,
                    isLive = request.isLive,
                    requestHeaders = request.requestHeaders,
                    mimeType = request.mimeType,
                    startPositionMs = if (request.isLive) C.TIME_UNSET else request.startPositionMs
                )
                isRecovering = false
                lastRearmAtMs = android.os.SystemClock.elapsedRealtime()
                Log.i(TAG, "[DECODER_RECOVERY] player rebuilt and stream re-armed")
            }, backoffMs)
        }
    }

    private fun degradeAudio(reason: String, renderer: String?) {
        if (audioDisabled || terminalReason != null || lastRequest == null) {
            return
        }
        audioDisabled = true
        val label = renderer?.let { "$reason in $it" } ?: reason
        Log.e(TAG, "[DECODER_RECOVERY] $label -> disabling the failing audio track; video continues")
        val current = player
        current.disableTrackType(C.TRACK_TYPE_AUDIO)
        if (lastRequest?.isLive == true) {
            current.seekToDefaultPosition()
        }
        current.prepare()
        current.playWhenReady = true
        onDegraded?.invoke("Audio unavailable: $label")
    }

    /**
     * Stop rebuilding and surface the failure. Leaving the stream silently dead — which is what
     * the previous unconditional loop did once its budget ran out — is the worst outcome for the
     * viewer, because nothing on screen says anything is wrong.
     */
    private fun giveUp(reason: String) {
        isRecovering = false
        terminalReason = reason
        watchdogEnabled = false
        handler.removeCallbacks(stallWatchdog)
        handler.removeCallbacks(healthyPlaybackReset)
        onUnrecoverable?.invoke(reason)
    }

    private fun allowRecovery(): Boolean {
        val now = android.os.SystemClock.elapsedRealtime()
        if (now - recoveryWindowStartMs > RECOVERY_WINDOW_MS) {
            recoveryWindowStartMs = now
            recoveryCount = 0
        }
        recoveryCount++
        return recoveryCount <= MAX_RECOVERIES_PER_WINDOW
    }

    fun playUrl(
        url: String,
        mediaId: String,
        title: String?,
        isLive: Boolean,
        requestHeaders: Map<String, String> = emptyMap(),
        mimeType: String? = null,
        startPositionMs: Long = 0L
    ) {
        val userInitiated = !isRecovering
        if (userInitiated) {
            requestGeneration += 1
        }
        lastRequest = PlaybackRequest(url, mediaId, title, isLive, requestHeaders, mimeType, startPositionMs)
        if (userInitiated) {
            // A fresh, user-initiated start: forget the previous stream's failure history.
            recoveryCount = 0
            consecutiveFastFailures = 0
            terminalReason = null
            if (audioDisabled) {
                audioDisabled = false
                player.enableTrackType(C.TRACK_TYPE_AUDIO)
            }
            lastRearmAtMs = android.os.SystemClock.elapsedRealtime()
        }

        player.stop()
        player.clearMediaItems()
        val mediaItemBuilder = MediaItem.Builder()
            .setUri(url)
            .setMediaId(mediaId)
            .setMediaMetadata(
                MediaMetadata.Builder()
                    .setTitle(title ?: mediaId)
                    .build()
            )
        if (!mimeType.isNullOrBlank()) {
            mediaItemBuilder.setMimeType(mimeType)
        }

        if (isLive) {
            mediaItemBuilder.setLiveConfiguration(
                MediaItem.LiveConfiguration.Builder()
                    .setTargetOffsetMs(LIVE_TARGET_OFFSET_MS)
                    .setMinOffsetMs(LIVE_MIN_OFFSET_MS)
                    .setMaxOffsetMs(LIVE_MAX_OFFSET_MS)
                    // Broadcast audio is AC-3 and goes out as HDMI passthrough, so it cannot
                    // follow a speed change. Letting media3 trim the playback speed to hold the
                    // live offset therefore skews video against audio and the video renderer
                    // starts holding frames back. Pin the speed and let it correct by seeking.
                    .setMinPlaybackSpeed(1.0f)
                    .setMaxPlaybackSpeed(1.0f)
                    .build()
            )
        }

        val mediaItem = mediaItemBuilder.build()
        val dataSourceFactory = OkHttpDataSource.Factory(okHttpClient)
        if (requestHeaders.isNotEmpty()) {
            dataSourceFactory.setDefaultRequestProperties(requestHeaders)
        }
        val mediaSource = DefaultMediaSourceFactory(dataSourceFactory)
            .createMediaSource(mediaItem)

        // C.TIME_UNSET starts at the window's default position, i.e. the live edge.
        player.setMediaSource(mediaSource, if (isLive && startPositionMs == 0L) C.TIME_UNSET else startPositionMs)
        player.prepare()
        player.playWhenReady = true

        resetWatchdogSample()
        watchdogEnabled = true
        handler.removeCallbacks(stallWatchdog)
        handler.postDelayed(stallWatchdog, WATCHDOG_INTERVAL_MS)
    }

    fun clear() {
        requestGeneration += 1
        watchdogEnabled = false
        handler.removeCallbacks(stallWatchdog)
        handler.removeCallbacks(healthyPlaybackReset)
        lastRequest = null
        isRecovering = false
        terminalReason = null
        recoveryCount = 0
        consecutiveFastFailures = 0
        resetWatchdogSample()
        player.stop()
        player.clearMediaItems()
    }

    fun release() {
        handler.removeCallbacksAndMessages(null)
        lastRequest = null
        onPlayerReplaced = null
        onUnrecoverable = null
        onDegraded = null
        player.release()
    }
}
