package io.github.manugh.xg2g.android.playback.ui

import android.content.res.Configuration
import android.os.Bundle
import android.view.View
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.isVisible
import androidx.lifecycle.lifecycleScope
import androidx.media3.ui.PlayerView
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.playback.PlaybackSession
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

class PlayerActivity : AppCompatActivity() {
    private val session: PlaybackSession by lazy(LazyThreadSafetyMode.NONE) {
        PlaybackSessionRegistry.getOrCreate(this)
    }

    private lateinit var playerView: PlayerView
    private lateinit var overlayView: View
    private lateinit var titleView: TextView
    private lateinit var statusView: TextView
    private var stateJob: Job? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_player)

        playerView = findViewById(R.id.player_view)
        overlayView = findViewById(R.id.player_overlay)
        titleView = findViewById(R.id.player_title)
        statusView = findViewById(R.id.player_status)
        playerView.player = session.player

        render(session.state.value)
    }

    override fun onStart() {
        super.onStart()
        playerView.player = session.player
        stateJob = lifecycleScope.launch {
            session.state.collect(::render)
        }
    }

    override fun onStop() {
        stateJob?.cancel()
        stateJob = null
        playerView.player = null
        super.onStop()
    }

    override fun onPictureInPictureModeChanged(
        isInPictureInPictureMode: Boolean,
        newConfig: Configuration
    ) {
        super.onPictureInPictureModeChanged(isInPictureInPictureMode, newConfig)
        session.updatePip(isInPictureInPictureMode)
        overlayView.isVisible = !isInPictureInPictureMode && shouldShowOverlay(session.state.value)
    }

    private fun render(state: NativePlaybackState) {
        titleView.text = when (val request = state.activeRequest) {
            null -> getString(R.string.native_playback_title)
            is io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest.Live ->
                request.title ?: request.serviceRef
            is io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest.Recording ->
                request.title ?: request.recordingId
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

        overlayView.isVisible = !isInPictureInPictureMode && shouldShowOverlay(state)
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
}
