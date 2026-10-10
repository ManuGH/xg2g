package io.github.manugh.xg2g.android.playback.ui

import android.content.res.Configuration
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.View
import android.widget.Button
import android.widget.ImageButton
import android.widget.ImageView
import android.widget.SeekBar
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.annotation.OptIn
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import androidx.media3.common.Player
import androidx.media3.common.util.UnstableApi
import androidx.media3.ui.PlayerView
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.ServerSettingsStore
import io.github.manugh.xg2g.android.playback.PlaybackSession
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy
import io.github.manugh.xg2g.android.transport.playback.loadPlaybackLogoBitmap
import java.util.Locale
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

@OptIn(markerClass = [UnstableApi::class])
class PlayerActivity : AppCompatActivity() {
    private val session: PlaybackSession by lazy(LazyThreadSafetyMode.NONE) {
        PlaybackSessionRegistry.getOrCreate(this)
    }

    private companion object {
        const val STABLE_PLAYBACK_DELAY_MS = 150L
        const val OSD_TIMEOUT_MS = 5000L
        const val HUD_TIMEOUT_MS = 1500L
        const val TICK_INTERVAL_MS = 500L
        const val LIVE_EDGE_THRESHOLD_MS = 6000L
    }

    private lateinit var playerView: PlayerView
    private lateinit var overlayView: View
    private lateinit var titleView: TextView
    private lateinit var statusView: TextView
    private lateinit var loadingOverlay: View
    private lateinit var loadingLogo: ImageView
    private lateinit var loadingTitle: TextView
    private lateinit var loadingSubtitle: TextView

    // OSD Views
    private lateinit var topBar: View
    private lateinit var btnClose: ImageButton
    private lateinit var feedbackHud: TextView
    private lateinit var osdBottom: View
    private lateinit var liveBadge: TextView
    private lateinit var timeOffset: TextView
    private lateinit var timePosition: TextView
    private lateinit var seekBar: SeekBar
    private lateinit var btnRewind: Button
    private lateinit var btnPlayPause: ImageButton
    private lateinit var btnForward: Button
    private lateinit var btnLive: Button
    private lateinit var programTitle: TextView
    private lateinit var channelName: TextView
    private lateinit var statsPill: TextView

    private var stateJob: Job? = null
    private var logoJob: Job? = null
    private var attachedPlayer: Player? = null
    private var isClosingPlayback = false
    private var loadingDismissed = false
    private var logoLoaded = false
    private var firstFrameRendered = false
    private var isOsdVisible = false
    private var isUserScrubbing = false

    private val handler = Handler(Looper.getMainLooper())

    private val stableDismissRunnable = Runnable { dismissLoadingOverlay() }
    private val hideOsdRunnable = Runnable { hideOsd() }
    private val hideHudRunnable = Runnable {
        feedbackHud.animate()
            .alpha(0f)
            .setDuration(200)
            .withEndAction { feedbackHud.isVisible = false }
            .start()
    }
    private val updateTickRunnable = object : Runnable {
        override fun run() {
            updateOsdState()
            if (isOsdVisible) {
                handler.postDelayed(this, TICK_INTERVAL_MS)
            }
        }
    }

    private val playerListener = object : Player.Listener {
        override fun onRenderedFirstFrame() {
            if (!firstFrameRendered) {
                firstFrameRendered = true
                scheduleStableDismiss()
            }
        }

        override fun onPlaybackStateChanged(playbackState: Int) {
            if (firstFrameRendered && playbackState == Player.STATE_READY) {
                scheduleStableDismiss()
            }
            if (playbackState == Player.STATE_BUFFERING && !loadingDismissed) {
                handler.removeCallbacks(stableDismissRunnable)
            }
            updatePlayPauseIcon()
        }

        override fun onIsPlayingChanged(isPlaying: Boolean) {
            if (firstFrameRendered && isPlaying && session.player.playbackState == Player.STATE_READY) {
                scheduleStableDismiss()
            }
            updatePlayPauseIcon()
        }
    }

    private fun scheduleStableDismiss() {
        handler.removeCallbacks(stableDismissRunnable)
        if (!loadingDismissed) {
            handler.postDelayed(stableDismissRunnable, STABLE_PLAYBACK_DELAY_MS)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_player)
        installBackHandler()

        playerView = findViewById(R.id.player_view)
        overlayView = findViewById(R.id.player_overlay)
        titleView = findViewById(R.id.player_title)
        statusView = findViewById(R.id.player_status)
        loadingOverlay = findViewById(R.id.player_loading_overlay)
        loadingLogo = findViewById(R.id.player_loading_logo)
        loadingTitle = findViewById(R.id.player_loading_title)
        loadingSubtitle = findViewById(R.id.player_loading_subtitle)

        // Bind OSD controls
        topBar = findViewById(R.id.player_top_bar)
        btnClose = findViewById(R.id.player_btn_close)
        feedbackHud = findViewById(R.id.player_feedback_hud)
        osdBottom = findViewById(R.id.player_osd_bottom)
        liveBadge = findViewById(R.id.player_live_badge)
        timeOffset = findViewById(R.id.player_time_offset)
        timePosition = findViewById(R.id.player_time_position)
        seekBar = findViewById(R.id.player_seekbar)
        btnRewind = findViewById(R.id.player_btn_rewind)
        btnPlayPause = findViewById(R.id.player_btn_play_pause)
        btnForward = findViewById(R.id.player_btn_forward)
        btnLive = findViewById(R.id.player_btn_live)
        programTitle = findViewById(R.id.player_program_title)
        channelName = findViewById(R.id.player_channel_name)
        statsPill = findViewById(R.id.player_stats_pill)

        playerView.useController = false
        // Decoder recovery swaps the player roughly every half minute. Without this the view
        // drops to its black shutter each time; keeping the last frame makes the ~1s gap read
        // as a brief freeze instead of a blackout. media3's own buffering spinner is disabled
        // in the layout for the same reason — the app shows its own overlay on first start.
        playerView.setKeepContentOnPlayerReset(true)
        playerView.player = session.player

        setupOsdListeners()
        showLoadingOverlay(session.state.value)
        render(session.state.value)
    }

    private fun setupOsdListeners() {
        playerView.setOnClickListener {
            if (isOsdVisible) {
                hideOsd()
            } else {
                showOsd()
            }
        }

        btnClose.setOnClickListener {
            requestPlaybackExit()
        }

        btnPlayPause.setOnClickListener {
            togglePlayPause()
        }

        btnRewind.setOnClickListener {
            seekDelta(-15_000L)
        }

        btnForward.setOnClickListener {
            seekDelta(15_000L)
        }

        btnLive.setOnClickListener {
            seekToLive()
        }

        seekBar.setOnSeekBarChangeListener(object : SeekBar.OnSeekBarChangeListener {
            override fun onProgressChanged(bar: SeekBar, progress: Int, fromUser: Boolean) {
                if (fromUser) {
                    val player = session.player
                    val duration = player.duration.takeIf { it > 0 } ?: 1000L
                    val targetMs = ((progress.toDouble() / 1000.0) * duration).toLong()
                    timePosition.text = formatDuration(targetMs)
                }
            }

            override fun onStartTrackingTouch(bar: SeekBar) {
                isUserScrubbing = true
                handler.removeCallbacks(hideOsdRunnable)
            }

            override fun onStopTrackingTouch(bar: SeekBar) {
                isUserScrubbing = false
                val player = session.player
                val duration = player.duration.takeIf { it > 0 } ?: 1000L
                val targetMs = ((bar.progress.toDouble() / 1000.0) * duration).toLong()
                player.seekTo(targetMs)
                resetOsdTimeout()
            }
        })

        seekBar.setOnKeyListener { _, keyCode, event ->
            if (keyCode == KeyEvent.KEYCODE_DPAD_LEFT || keyCode == KeyEvent.KEYCODE_DPAD_RIGHT) {
                resetOsdTimeout()
                if (event.action == KeyEvent.ACTION_UP) {
                    val player = session.player
                    val duration = player.duration.takeIf { it > 0 } ?: 1000L
                    val targetMs = ((seekBar.progress.toDouble() / 1000.0) * duration).toLong()
                    player.seekTo(targetMs)
                }
                false
            } else if ((keyCode == KeyEvent.KEYCODE_DPAD_CENTER || keyCode == KeyEvent.KEYCODE_ENTER || keyCode == KeyEvent.KEYCODE_NUMPAD_ENTER) && event.action == KeyEvent.ACTION_UP) {
                val player = session.player
                val duration = player.duration.takeIf { it > 0 } ?: 1000L
                val targetMs = ((seekBar.progress.toDouble() / 1000.0) * duration).toLong()
                player.seekTo(targetMs)
                resetOsdTimeout()
                true
            } else {
                false
            }
        }
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (isOsdVisible) {
                    hideOsd()
                } else {
                    requestPlaybackExit()
                }
            }
        })
    }

    override fun onStart() {
        super.onStart()
        attachPlayer()
        stateJob = lifecycleScope.launch {
            session.state.collect(::render)
        }
    }

    override fun onStop() {
        stateJob?.cancel()
        stateJob = null
        logoJob?.cancel()
        logoJob = null
        handler.removeCallbacks(stableDismissRunnable)
        handler.removeCallbacks(hideOsdRunnable)
        handler.removeCallbacks(hideHudRunnable)
        handler.removeCallbacks(updateTickRunnable)
        detachPlayer()
        super.onStop()
    }

    /**
     * Decoder recovery replaces the whole ExoPlayer instance, so the view must bind to the
     * current one rather than to whatever it captured at onStart.
     */
    private fun attachPlayer() {
        val current = session.player
        if (attachedPlayer === current) {
            return
        }
        attachedPlayer?.removeListener(playerListener)
        attachedPlayer = current
        playerView.player = current
        current.addListener(playerListener)
    }

    private fun detachPlayer() {
        attachedPlayer?.removeListener(playerListener)
        attachedPlayer = null
        playerView.player = null
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.action == KeyEvent.ACTION_DOWN) {
            if (isExitKey(event)) {
                requestPlaybackExit()
                return true
            }
            when (event.keyCode) {
                KeyEvent.KEYCODE_BACK -> {
                    if (isOsdVisible) {
                        hideOsd()
                        return true
                    } else {
                        requestPlaybackExit()
                        return true
                    }
                }
                KeyEvent.KEYCODE_DPAD_CENTER,
                KeyEvent.KEYCODE_ENTER,
                KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                    if (!isOsdVisible) {
                        showOsd()
                        btnPlayPause.requestFocus()
                        return true
                    }
                    resetOsdTimeout()
                }
                KeyEvent.KEYCODE_DPAD_UP -> {
                    if (!isOsdVisible) {
                        showOsd()
                        btnPlayPause.requestFocus()
                        return true
                    }
                    resetOsdTimeout()
                }
                KeyEvent.KEYCODE_DPAD_DOWN -> {
                    if (isOsdVisible) {
                        hideOsd()
                        return true
                    }
                }
                KeyEvent.KEYCODE_DPAD_LEFT -> {
                    if (!isOsdVisible) {
                        seekDelta(-15_000L)
                        return true
                    }
                    resetOsdTimeout()
                }
                KeyEvent.KEYCODE_DPAD_RIGHT -> {
                    if (!isOsdVisible) {
                        seekDelta(15_000L)
                        return true
                    }
                    resetOsdTimeout()
                }
                KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE,
                KeyEvent.KEYCODE_HEADSETHOOK -> {
                    togglePlayPause()
                    return true
                }
                KeyEvent.KEYCODE_MEDIA_PLAY -> {
                    session.player.play()
                    updatePlayPauseIcon()
                    showOsd()
                    return true
                }
                KeyEvent.KEYCODE_MEDIA_PAUSE -> {
                    session.player.pause()
                    updatePlayPauseIcon()
                    showOsd()
                    return true
                }
                KeyEvent.KEYCODE_MEDIA_FAST_FORWARD,
                KeyEvent.KEYCODE_MEDIA_SKIP_FORWARD,
                KeyEvent.KEYCODE_MEDIA_STEP_FORWARD -> {
                    seekDelta(15_000L)
                    return true
                }
                KeyEvent.KEYCODE_MEDIA_REWIND,
                KeyEvent.KEYCODE_MEDIA_SKIP_BACKWARD,
                KeyEvent.KEYCODE_MEDIA_STEP_BACKWARD,
                KeyEvent.KEYCODE_MEDIA_PREVIOUS -> {
                    seekDelta(-15_000L)
                    return true
                }
            }
        }
        return super.dispatchKeyEvent(event)
    }

    override fun onPictureInPictureModeChanged(
        isInPictureInPictureMode: Boolean,
        newConfig: Configuration
    ) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        session.updatePip(isInPictureInPictureMode)
        overlayView.isVisible = !isInPictureInPictureMode && shouldShowOverlay(session.state.value)
        loadingOverlay.isVisible = !isInPictureInPictureMode && !loadingDismissed
        if (isInPictureInPictureMode) {
            hideOsd()
        }
    }

    private fun showOsd(autoHide: Boolean = true) {
        if (isInPictureInPictureMode) return
        topBar.animate().cancel()
        osdBottom.animate().cancel()
        topBar.alpha = 1f
        topBar.isVisible = true
        osdBottom.alpha = 1f
        osdBottom.isVisible = true
        isOsdVisible = true
        updateOsdState()
        handler.removeCallbacks(updateTickRunnable)
        handler.post(updateTickRunnable)
        if (autoHide) {
            resetOsdTimeout()
        } else {
            handler.removeCallbacks(hideOsdRunnable)
        }
    }

    private fun hideOsd() {
        if (!isOsdVisible) return
        isOsdVisible = false
        handler.removeCallbacks(hideOsdRunnable)
        handler.removeCallbacks(updateTickRunnable)
        topBar.animate()
            .alpha(0f)
            .setDuration(250)
            .withEndAction { topBar.isVisible = false }
            .start()
        osdBottom.animate()
            .alpha(0f)
            .setDuration(250)
            .withEndAction { osdBottom.isVisible = false }
            .start()
    }

    private fun resetOsdTimeout() {
        handler.removeCallbacks(hideOsdRunnable)
        if (isOsdVisible && !isUserScrubbing) {
            handler.postDelayed(hideOsdRunnable, OSD_TIMEOUT_MS)
        }
    }

    private fun showFeedback(message: String) {
        feedbackHud.animate().cancel()
        feedbackHud.text = message
        feedbackHud.alpha = 1f
        feedbackHud.isVisible = true
        handler.removeCallbacks(hideHudRunnable)
        handler.postDelayed(hideHudRunnable, HUD_TIMEOUT_MS)
    }

    private fun togglePlayPause() {
        val player = session.player
        if (player.isPlaying) {
            player.pause()
            showFeedback("⏸ Pause")
        } else {
            player.play()
            showFeedback("▶ Play")
        }
        updatePlayPauseIcon()
        showOsd()
    }

    private fun updatePlayPauseIcon() {
        if (::btnPlayPause.isInitialized) {
            btnPlayPause.setImageResource(
                if (session.player.isPlaying) R.drawable.ic_player_pause else R.drawable.ic_player_play
            )
        }
    }

    private fun seekDelta(deltaMs: Long) {
        val player = session.player
        val current = player.currentPosition
        val target = (current + deltaMs).coerceAtLeast(0L)
        player.seekTo(target)
        showFeedback(if (deltaMs < 0) "↶ ${-deltaMs / 1000}s" else "↷ ${deltaMs / 1000}s")
        updateOsdState()
        showOsd()
    }

    private fun seekToLive() {
        session.player.seekToDefaultPosition()
        showFeedback("→ LIVE")
        updateOsdState()
        showOsd()
    }

    private fun updateOsdState() {
        val player = session.player
        updatePlayPauseIcon()

        val request = session.state.value.activeRequest
        val isLive = request is NativePlaybackRequest.Live || player.isCurrentMediaItemLive

        if (isLive) {
            val liveOffsetMs = if (player.isCurrentMediaItemLive) player.currentLiveOffset else -1L
            val effectiveOffsetMs = if (liveOffsetMs >= 0) liveOffsetMs else {
                val duration = player.duration
                if (duration > 0) (duration - player.currentPosition).coerceAtLeast(0L) else 0L
            }

            if (effectiveOffsetMs <= LIVE_EDGE_THRESHOLD_MS) {
                liveBadge.isVisible = true
                timeOffset.isVisible = false
                btnLive.isEnabled = false
                btnLive.alpha = 0.45f
            } else {
                liveBadge.isVisible = false
                timeOffset.isVisible = true
                val secBehind = (effectiveOffsetMs / 1000L).coerceAtLeast(1L)
                timeOffset.text = getString(R.string.player_behind_live_format, secBehind)
                btnLive.isEnabled = true
                btnLive.alpha = 1.0f
            }

            btnLive.isVisible = true

            if (!isUserScrubbing) {
                val duration = player.duration.takeIf { it > 0 } ?: 1000L
                val position = player.currentPosition.coerceIn(0L, duration)
                seekBar.progress = ((position.toDouble() / duration.toDouble()) * 1000).toInt()
                val buffered = player.bufferedPosition.coerceIn(0L, duration)
                seekBar.secondaryProgress = ((buffered.toDouble() / duration.toDouble()) * 1000).toInt()
                timePosition.text = formatDuration(position)
            }
        } else {
            liveBadge.isVisible = false
            timeOffset.isVisible = false
            btnLive.isVisible = false

            val duration = player.duration.coerceAtLeast(1L)
            val position = player.currentPosition.coerceIn(0L, duration)

            if (!isUserScrubbing) {
                seekBar.progress = ((position.toDouble() / duration.toDouble()) * 1000).toInt()
                val buffered = player.bufferedPosition.coerceIn(0L, duration)
                seekBar.secondaryProgress = ((buffered.toDouble() / duration.toDouble()) * 1000).toInt()
                timePosition.text = "${formatDuration(position)} / ${formatDuration(duration)}"
            }
        }

        // Program and channel titles
        when (request) {
            is NativePlaybackRequest.Live -> {
                channelName.text = request.title ?: request.serviceRef
                programTitle.text = request.title ?: request.serviceRef
            }
            is NativePlaybackRequest.Recording -> {
                channelName.text = getString(R.string.tv_destination_recordings)
                programTitle.text = request.title ?: request.recordingId
            }
            null -> {
                channelName.text = getString(R.string.native_playback_title)
                programTitle.text = getString(R.string.native_playback_title)
            }
        }

        // Telemetry stats
        val liveOffsetMs = if (player.isCurrentMediaItemLive) player.currentLiveOffset else -1L
        if (liveOffsetMs > 0) {
            val seconds = liveOffsetMs / 1000.0
            statsPill.text = String.format(Locale.US, "⚡ %.1f s", seconds)
        } else {
            val mode = session.state.value.diagnostics?.playbackMode?.wireValue?.uppercase() ?: "DIRECT"
            statsPill.text = "⚡ $mode"
        }
    }

    private fun formatDuration(ms: Long): String {
        val totalSec = (ms / 1000L).coerceAtLeast(0L)
        val hours = totalSec / 3600L
        val minutes = (totalSec % 3600L) / 60L
        val seconds = totalSec % 60L
        return if (hours > 0) {
            String.format(Locale.US, "%02d:%02d:%02d", hours, minutes, seconds)
        } else {
            String.format(Locale.US, "%02d:%02d", minutes, seconds)
        }
    }

    private fun showLoadingOverlay(state: NativePlaybackState) {
        loadingDismissed = false
        firstFrameRendered = false
        logoLoaded = false
        loadingOverlay.alpha = 1f
        loadingOverlay.isVisible = true
        loadingLogo.setImageResource(R.drawable.xg2g_logo_mono_dark)
        loadingLogo.alpha = 0.9f
        loadingLogo.isVisible = true

        val request = state.activeRequest
        loadingTitle.text = when (request) {
            is NativePlaybackRequest.Live -> request.title ?: request.serviceRef
            is NativePlaybackRequest.Recording -> request.title ?: request.recordingId
            null -> getString(R.string.native_playback_title)
        }
        loadingSubtitle.text = getString(R.string.native_playback_status_loading)

        loadLogo(request?.logoUrl)
    }

    private fun loadLogo(url: String?) {
        if (logoLoaded) return
        if (url.isNullOrBlank()) return

        logoLoaded = true
        logoJob?.cancel()
        logoJob = lifecycleScope.launch {
            val bitmap = loadPlaybackLogoBitmap(
                ServerSettingsStore(this@PlayerActivity).getServerUrl(),
                url
            )
            if (bitmap != null && !loadingDismissed) {
                loadingLogo.setImageBitmap(bitmap)
                loadingLogo.alpha = 1f
                loadingLogo.isVisible = true
            }
        }
    }

    private fun dismissLoadingOverlay() {
        if (loadingDismissed) return
        loadingDismissed = true
        handler.removeCallbacks(stableDismissRunnable)
        logoJob?.cancel()
        loadingOverlay.animate()
            .alpha(0f)
            .setDuration(400)
            .withEndAction { loadingOverlay.isVisible = false }
            .start()

        // Briefly show OSD when playback commences
        showOsd(autoHide = true)
    }

    private fun render(state: NativePlaybackState) {
        attachPlayer()
        if (
            firstFrameRendered &&
            loadingDismissed &&
            state.lastError.isNullOrBlank() &&
            state.playbackWarning.isNullOrBlank()
        ) {
            overlayView.isVisible = false
            return
        }
        titleView.text = when (val request = state.activeRequest) {
            null -> getString(R.string.native_playback_title)
            is NativePlaybackRequest.Live -> request.title ?: request.serviceRef
            is NativePlaybackRequest.Recording -> request.title ?: request.recordingId
        }

        statusView.text = when {
            !state.lastError.isNullOrBlank() ->
                ErrorPresentationPolicy.formatPlaybackError(state.lastError, this)

            !state.playbackWarning.isNullOrBlank() ->
                ErrorPresentationPolicy.formatPlaybackWarning(state.playbackWarning, this)

            state.session != null ->
                ErrorPresentationPolicy.formatPlaybackSessionState(state.session.state.wireValue, this)

            state.activeRequest != null -> getString(R.string.native_playback_status_loading)
            else -> getString(R.string.native_playback_status_idle)
        }

        if (!loadingDismissed) {
            val request = state.activeRequest
            loadingTitle.text = when (request) {
                is NativePlaybackRequest.Live -> request.title ?: request.serviceRef
                is NativePlaybackRequest.Recording -> request.title ?: request.recordingId
                null -> getString(R.string.native_playback_title)
            }
            loadingSubtitle.text = statusView.text
            loadLogo(request?.logoUrl)
        }

        overlayView.isVisible = !isInPictureInPictureMode && shouldShowOverlay(state) && loadingDismissed
    }

    private fun shouldShowOverlay(state: NativePlaybackState): Boolean {
        // An error outranks everything: playback that dies after the first frame used to leave the
        // last decoded picture frozen on screen with nothing telling the viewer it had stopped.
        if (!state.lastError.isNullOrBlank()) {
            return true
        }
        if (!state.playbackWarning.isNullOrBlank()) {
            return true
        }
        if (firstFrameRendered || session.player.isPlaying) {
            return false
        }
        if (state.session != null) {
            return false
        }
        return true
    }

    private fun requestPlaybackExit() {
        if (isClosingPlayback) {
            return
        }
        isClosingPlayback = true
        runCatching {
            session.player.stop()
            session.player.clearMediaItems()
        }
        NativePlaybackBridge(this).stop()
        finish()
    }

    override fun onDestroy() {
        if (!isClosingPlayback) {
            isClosingPlayback = true
            runCatching {
                session.player.stop()
                session.player.clearMediaItems()
            }
            NativePlaybackBridge(this).stop()
        }
        super.onDestroy()
    }

    private fun isExitKey(event: KeyEvent): Boolean {
        if (event.repeatCount != 0) {
            return false
        }
        return when (event.keyCode) {
            KeyEvent.KEYCODE_ESCAPE,
            KeyEvent.KEYCODE_MEDIA_STOP -> event.action == KeyEvent.ACTION_DOWN
            else -> false
        }
    }
}
