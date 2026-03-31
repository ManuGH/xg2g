package io.github.manugh.xg2g.android.playback.model

import androidx.media3.common.Player

data class NativePlaybackState(
    val activeRequest: NativePlaybackRequest? = null,
    val session: SessionSnapshot? = null,
    val diagnostics: NativePlaybackDiagnostics? = null,
    val playerState: Int = Player.STATE_IDLE,
    val playWhenReady: Boolean = false,
    val isInPip: Boolean = false,
    val lastError: String? = null
)
