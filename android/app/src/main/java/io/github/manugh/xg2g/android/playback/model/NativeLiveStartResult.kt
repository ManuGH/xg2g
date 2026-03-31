package io.github.manugh.xg2g.android.playback.model

data class NativeLiveStartResult(
    val sessionId: String,
    val diagnostics: NativePlaybackDiagnostics?
)
