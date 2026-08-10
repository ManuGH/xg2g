package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativePlaybackDiagnostics
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.net.PlaybackApi
import kotlinx.coroutines.CancellationException

internal class LiveSessionCoordinator(
    private val playbackApi: PlaybackApi,
    private val readinessPoller: ReadinessPoller,
    private val heartbeatManager: HeartbeatManager,
    private val onSessionUpdated: (SessionSnapshot) -> Unit,
    private val onDiagnosticsUpdated: (NativePlaybackDiagnostics) -> Unit,
    private val onError: (Throwable) -> Unit
) {
    private var activeSessionId: String? = null

    suspend fun start(request: NativePlaybackRequest.Live): SessionSnapshot {
        stop(activeSessionId)
        playbackApi.ensureAuthSession(request.authToken)
        val startResult = playbackApi.startLiveIntent(request)
        startResult.diagnostics?.let(onDiagnosticsUpdated)
        val sessionId = startResult.sessionId
        activeSessionId = sessionId
        return try {
            val snapshot = readinessPoller.awaitReady(sessionId)
            val finalSnapshot = if (snapshot.playbackUrl.isNullOrBlank() && !startResult.streamUrl.isNullOrBlank()) {
                snapshot.copy(playbackUrl = startResult.streamUrl)
            } else {
                snapshot
            }
            onSessionUpdated(finalSnapshot)

            heartbeatManager.start(
                sessionId = sessionId,
                intervalSeconds = finalSnapshot.heartbeatIntervalSec ?: HEARTBEAT_FALLBACK_SECONDS,
                onSessionUpdated = onSessionUpdated,
                onError = onError
            )

            finalSnapshot
        } catch (error: CancellationException) {
            cleanupStartedSession(sessionId)
            throw error
        } catch (error: Throwable) {
            cleanupStartedSession(sessionId)
            throw error
        }
    }

    suspend fun stop(sessionId: String? = null) {
        val targetSessionId = sessionId ?: activeSessionId
        activeSessionId = null
        heartbeatManager.stop()
        if (targetSessionId != null) {
            runCatching { playbackApi.stopSession(targetSessionId) }
                .onFailure(onError)
        }
    }

    private suspend fun cleanupStartedSession(sessionId: String) {
        heartbeatManager.stop()
        runCatching { playbackApi.stopSession(sessionId) }
            .onFailure(onError)
    }

    private companion object {
        private const val HEARTBEAT_FALLBACK_SECONDS = 5
    }
}
