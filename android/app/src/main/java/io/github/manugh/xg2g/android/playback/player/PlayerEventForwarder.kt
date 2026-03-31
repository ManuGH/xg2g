package io.github.manugh.xg2g.android.playback.player

import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player

internal class PlayerEventForwarder(
    private val player: Player,
    private val onStateChanged: (playerState: Int, playWhenReady: Boolean, error: String?) -> Unit
) : Player.Listener {

    init {
        player.addListener(this)
        onStateChanged(player.playbackState, player.playWhenReady, null)
    }

    override fun onPlaybackStateChanged(playbackState: Int) {
        onStateChanged(playbackState, player.playWhenReady, null)
    }

    override fun onPlayWhenReadyChanged(playWhenReady: Boolean, reason: Int) {
        onStateChanged(player.playbackState, playWhenReady, null)
    }

    override fun onMediaItemTransition(mediaItem: MediaItem?, reason: Int) {
        onStateChanged(player.playbackState, player.playWhenReady, null)
    }

    override fun onPlayerError(error: PlaybackException) {
        onStateChanged(player.playbackState, player.playWhenReady, error.message ?: "Playback error")
    }

    fun dispose() {
        player.removeListener(this)
    }
}
