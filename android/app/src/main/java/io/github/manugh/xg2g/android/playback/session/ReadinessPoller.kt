package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.net.PlaybackApi
import io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo
import kotlinx.coroutines.delay

internal class ReadinessPoller(
    private val playbackApi: PlaybackApi,
    private val errorMapper: PlaybackErrorMapper
) {
    suspend fun awaitReady(
        sessionId: String,
        maxAttempts: Int = 80,
        pollMs: Long = 500L
    ): SessionSnapshot {
        repeat(maxAttempts) {
            val snapshot = playbackApi.getSessionState(sessionId)
            if (!snapshot.playbackUrl.isNullOrBlank()) {
                return snapshot
            }
            if (snapshot.state.isTerminal) {
                throw errorMapper.toSessionStateException(snapshot)
            }
            delay(pollMs)
        }

        throw IllegalStateException("Session $sessionId did not become ready in ${maxAttempts * pollMs}ms")
    }

    suspend fun awaitRecordingPlaylist(
        recordingId: String,
        maxAttempts: Int = 80,
        pollMs: Long = 500L
    ): String {
        repeat(maxAttempts) {
            playbackApi.getRecordingPlaylistIfReady(recordingId)?.let { return it }
            delay(pollMs)
        }

        throw IllegalStateException("Recording $recordingId playlist did not become ready in ${maxAttempts * pollMs}ms")
    }

    suspend fun awaitRecordingPlayback(
        request: NativePlaybackRequest.Recording,
        maxAttempts: Int = 80,
        pollMs: Long = 500L
    ): NativeRecordingPlaybackInfo {
        var playbackInfo: NativeRecordingPlaybackInfo? = null
        repeat(maxAttempts) {
            if (playbackInfo == null) {
                playbackInfo = playbackApi.getRecordingPlaybackInfo(request)
            }
            val readyPlayback = playbackInfo
            if (readyPlayback != null) {
                playbackApi.getPlaybackUrlIfReady(readyPlayback.playbackUrl)?.let {
                    return readyPlayback.copy(playbackUrl = it)
                }
            }
            delay(pollMs)
        }

        throw IllegalStateException("Recording ${request.recordingId} playback did not become ready in ${maxAttempts * pollMs}ms")
    }
}
