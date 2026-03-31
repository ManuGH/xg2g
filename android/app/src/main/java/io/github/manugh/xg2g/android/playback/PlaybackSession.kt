package io.github.manugh.xg2g.android.playback

import androidx.media3.common.Player
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import kotlinx.coroutines.flow.StateFlow

internal interface PlaybackSession {
    val player: Player
    val state: StateFlow<NativePlaybackState>

    suspend fun start(request: NativePlaybackRequest)
    suspend fun stop(force: Boolean = false)
    fun updatePip(isInPip: Boolean)
    fun reportCommandFailure(throwable: Throwable)
    fun close()
}
