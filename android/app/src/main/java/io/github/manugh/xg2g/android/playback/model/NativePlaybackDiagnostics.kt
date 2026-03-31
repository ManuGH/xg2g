package io.github.manugh.xg2g.android.playback.model

data class NativePlaybackDiagnostics(
    val requestId: String? = null,
    val playbackMode: PlaybackMode? = null,
    val profileReason: String? = null,
    val capHash: String? = null,
    val playbackInfoJson: String? = null,
    val traceJson: String? = null
) {
    fun mergeSession(session: SessionSnapshot): NativePlaybackDiagnostics = copy(
        requestId = session.requestId ?: requestId,
        profileReason = session.profileReason ?: profileReason,
        traceJson = session.traceJson ?: traceJson
    )
}
