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
    suspend fun start(request: NativePlaybackRequest.Live): SessionSnapshot {
        playbackApi.ensureAuthSession(request.authToken)
        val startResult = playbackApi.startLiveIntent(request)
        startResult.diagnostics?.let(onDiagnosticsUpdated)
        val sessionId = startResult.sessionId
        return try {
            val snapshot = readinessPoller.awaitReady(sessionId)
            onSessionUpdated(snapshot)

            heartbeatManager.start(
                sessionId = sessionId,
                intervalSeconds = snapshot.heartbeatIntervalSec ?: HEARTBEAT_FALLBACK_SECONDS,
                onSessionUpdated = onSessionUpdated,
                onError = onError
            )

            snapshot
        } catch (error: CancellationException) {
            cleanupStartedSession(sessionId)
            throw error
        } catch (error: Throwable) {
            cleanupStartedSession(sessionId)
            throw error
        }
    }

    suspend fun stop(sessionId: String?) {
        heartbeatManager.stop()
        if (sessionId != null) {
            runCatching { playbackApi.stopSession(sessionId) }
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
