package io.github.manugh.xg2g.android.playback.net

import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot

internal interface PlaybackApi {
    suspend fun ensureAuthSession(authToken: String?)
    suspend fun startLiveIntent(request: NativePlaybackRequest.Live): NativeLiveStartResult
    suspend fun getSessionState(sessionId: String): SessionSnapshot
    suspend fun getRecordingPlaybackInfo(request: NativePlaybackRequest.Recording): NativeRecordingPlaybackInfo?
    suspend fun getRecordingPlaylistIfReady(recordingId: String): String?
    suspend fun getPlaybackUrlIfReady(playbackUrl: String): String?
    suspend fun heartbeat(sessionId: String): SessionSnapshot
    suspend fun reportPlaybackFeedback(sessionId: String, event: String, code: Int?, message: String?)
    suspend fun stopSession(sessionId: String)
    fun sessionPlaylistUrl(sessionId: String): String
    fun recordingPlaylistUrl(recordingId: String): String
}

internal data class NativeRecordingPlaybackInfo(
    val playbackUrl: String,
    val requestId: String? = null,
    val selectedOutputKind: String? = null,
    val decisionMode: String? = null,
    val mimeType: String? = null
)
