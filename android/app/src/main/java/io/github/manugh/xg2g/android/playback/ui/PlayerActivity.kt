package io.github.manugh.xg2g.android.playback.ui

import android.content.res.Configuration
import android.graphics.BitmapFactory
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.View
import android.webkit.CookieManager
import android.widget.ImageView
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import androidx.media3.common.Player
import androidx.media3.ui.PlayerView
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.ServerSettingsStore
import io.github.manugh.xg2g.android.playback.PlaybackSession
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.net.HttpURLConnection
import java.net.URL

class PlayerActivity : AppCompatActivity() {
    private val session: PlaybackSession by lazy(LazyThreadSafetyMode.NONE) {
        PlaybackSessionRegistry.getOrCreate(this)
    }

    private companion object {
        const val STABLE_PLAYBACK_DELAY_MS = 1500L
    }

    private lateinit var playerView: PlayerView
    private lateinit var overlayView: View
    private lateinit var titleView: TextView
    private lateinit var statusView: TextView
    private lateinit var loadingOverlay: View
    private lateinit var loadingLogo: ImageView
    private lateinit var loadingTitle: TextView
    private lateinit var loadingSubtitle: TextView
    private var stateJob: Job? = null
    private var logoJob: Job? = null
    private var isClosingPlayback = false
    private var loadingDismissed = false
    private var logoLoaded = false
    private var firstFrameRendered = false
    private val handler = Handler(Looper.getMainLooper())

    private val stableDismissRunnable = Runnable { dismissLoadingOverlay() }

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
        }

        override fun onIsPlayingChanged(isPlaying: Boolean) {
            if (firstFrameRendered && isPlaying && session.player.playbackState == Player.STATE_READY) {
                scheduleStableDismiss()
            }
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

        playerView.useController = false
        playerView.player = session.player

        showLoadingOverlay(session.state.value)
        render(session.state.value)
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                requestPlaybackExit()
            }
        })
    }

    override fun onStart() {
        super.onStart()
        playerView.player = session.player
        session.player.addListener(playerListener)
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
        session.player.removeListener(playerListener)
        playerView.player = null
        super.onStop()
    }

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (isExitKey(event)) {
            requestPlaybackExit()
            return true
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

        val absoluteUrl = if (url.startsWith("/")) {
            val base = ServerSettingsStore(this).getServerUrl()?.trimEnd('/') ?: return
            "$base$url"
        } else {
            url
        }

        logoLoaded = true
        logoJob?.cancel()
        logoJob = lifecycleScope.launch {
            val bitmap = withContext(Dispatchers.IO) {
                runCatching {
                    val conn = URL(absoluteUrl).openConnection() as HttpURLConnection
                    val cookies = CookieManager.getInstance().getCookie(absoluteUrl)
                    if (!cookies.isNullOrBlank()) {
                        conn.setRequestProperty("Cookie", cookies)
                    }
                    conn.connectTimeout = 5_000
                    conn.readTimeout = 5_000
                    conn.inputStream.use { BitmapFactory.decodeStream(it) }
                }.getOrNull()
            }
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
    }

    private fun render(state: NativePlaybackState) {
        titleView.text = when (val request = state.activeRequest) {
            null -> getString(R.string.native_playback_title)
            is NativePlaybackRequest.Live -> request.title ?: request.serviceRef
            is NativePlaybackRequest.Recording -> request.title ?: request.recordingId
        }

        statusView.text = when {
            !state.lastError.isNullOrBlank() ->
                getString(R.string.native_playback_status_error, state.lastError)

            state.session != null ->
                getString(
                    R.string.native_playback_status_active,
                    state.session.state.wireValue
                )

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
        if (!state.lastError.isNullOrBlank()) {
            return true
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
        NativePlaybackBridge(this).stop()
        finish()
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
