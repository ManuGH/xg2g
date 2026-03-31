package io.github.manugh.xg2g.android.playback

import android.content.Context
import android.util.Log
import androidx.media3.common.Player
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import io.github.manugh.xg2g.android.playback.model.NativePlaybackDiagnostics
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.SessionMode
import io.github.manugh.xg2g.android.playback.model.SessionState
import io.github.manugh.xg2g.android.playback.net.PlaybackApiClient
import io.github.manugh.xg2g.android.playback.player.PlayerEventForwarder
import io.github.manugh.xg2g.android.playback.player.PlayerHolder
import io.github.manugh.xg2g.android.playback.session.HeartbeatManager
import io.github.manugh.xg2g.android.playback.session.LiveSessionCoordinator
import io.github.manugh.xg2g.android.playback.session.PlaybackErrorMapper
import io.github.manugh.xg2g.android.playback.session.ReadinessPoller
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import java.util.UUID

internal class PlaybackRuntime(
    context: Context,
    private val stateStore: PlaybackStateStore
) : PlaybackSession {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val playbackApi = PlaybackApiClient(context.applicationContext)
    private val playerHolder = PlayerHolder(context.applicationContext, playbackApi.okHttpClient)
    private val heartbeatManager = HeartbeatManager(playbackApi, scope)
    private val readinessPoller = ReadinessPoller(playbackApi, PlaybackErrorMapper())
    private val liveSessionCoordinator = LiveSessionCoordinator(
        playbackApi = playbackApi,
        readinessPoller = readinessPoller,
        heartbeatManager = heartbeatManager,
        onSessionUpdated = ::updateSession,
        onDiagnosticsUpdated = ::updateDiagnostics,
        onError = ::reportError
    )
    private val playerEventForwarder = PlayerEventForwarder(playerHolder.player, ::onPlayerStateChanged)
    private var reportedReadySessionId: String? = null
    private var reportedErrorSignature: String? = null

    override val player = playerHolder.player
    override val state: StateFlow<NativePlaybackState> = stateStore.state

    override suspend fun start(request: NativePlaybackRequest) {
        stop(force = true)
        setState(NativePlaybackState(activeRequest = request))

        when (request) {
            is NativePlaybackRequest.Live -> {
                val snapshot = liveSessionCoordinator.start(request)
                val playbackUrl = playbackApi.resolvePlaybackUrl(
                    snapshot.playbackUrl ?: playbackApi.sessionPlaylistUrl(snapshot.sessionId)
                )
                playerHolder.playUrl(
                    url = playbackUrl,
                    mediaId = snapshot.sessionId,
                    title = request.title ?: request.serviceRef,
                    isLive = true,
                    requestHeaders = playbackApi.playbackRequestHeaders(playbackUrl)
                )
                updateSession(snapshot)
            }

            is NativePlaybackRequest.Recording -> {
                playbackApi.ensureAuthSession(request.authToken)
                heartbeatManager.stop()
                val readyPlayback = readinessPoller.awaitRecordingPlayback(request)
                val playbackUrl = playbackApi.resolvePlaybackUrl(readyPlayback.playbackUrl)
                val snapshot = SessionSnapshot(
                    sessionId = "rec:${request.recordingId}",
                    state = SessionState.Ready,
                    playbackUrl = playbackUrl,
                    mode = SessionMode.Recording,
                    requestId = null,
                    profileReason = null,
                    traceJson = null,
                    heartbeatIntervalSec = null,
                    leaseExpiresAt = null,
                    durationSeconds = null,
                    seekableStartSeconds = null,
                    seekableEndSeconds = null,
                    liveEdgeSeconds = null
                )
                playerHolder.playUrl(
                    url = playbackUrl,
                    mediaId = snapshot.sessionId,
                    title = request.title ?: request.recordingId,
                    isLive = false,
                    requestHeaders = playbackApi.playbackRequestHeaders(playbackUrl),
                    mimeType = readyPlayback.mimeType,
                    startPositionMs = request.startPositionMs
                )
                updateSession(snapshot)
            }
        }
    }

    override suspend fun stop(force: Boolean) {
        val current = stateStore.current()
        when (current.activeRequest) {
            is NativePlaybackRequest.Live -> liveSessionCoordinator.stop(current.session?.sessionId)
            is NativePlaybackRequest.Recording, null -> heartbeatManager.stop()
        }
        playerHolder.clear()
        reportedReadySessionId = null
        reportedErrorSignature = null
        if (force || current.activeRequest != null || current.session != null) {
            setState(NativePlaybackState())
        }
    }

    override fun updatePip(isInPip: Boolean) {
        mutateState { current -> current.copy(isInPip = isInPip) }
    }

    override fun reportCommandFailure(throwable: Throwable) {
        reportError(throwable)
    }

    override fun close() {
        heartbeatManager.stop()
        playerEventForwarder.dispose()
        playerHolder.release()
        setState(NativePlaybackState())
        scope.cancel()
    }

    private fun updateSession(snapshot: SessionSnapshot) {
        mutateState { current ->
            current.copy(
                session = snapshot,
                diagnostics = current.diagnostics?.mergeSession(snapshot),
                lastError = null
            )
        }
    }

    private fun updateDiagnostics(diagnostics: NativePlaybackDiagnostics) {
        mutateState { current ->
            current.copy(
                diagnostics = diagnostics,
                lastError = null
            )
        }
    }

    private fun reportError(throwable: Throwable) {
        Log.e(TAG, "native playback error", throwable)
        reportSessionFeedback("error", null, throwable.message ?: throwable.javaClass.simpleName)
        mutateState { current ->
            current.copy(lastError = throwable.message ?: throwable.javaClass.simpleName)
        }
    }

    private fun onPlayerStateChanged(playerState: Int, playWhenReady: Boolean, error: String?) {
        if (error != null) {
            val signature = currentLiveSessionId()?.let { "$it::$error" }
            if (signature != null && signature != reportedErrorSignature) {
                reportedErrorSignature = signature
                reportSessionFeedback("error", null, error)
            }
        }
        if (playerState == Player.STATE_READY && playWhenReady) {
            currentLiveSessionId()?.let { sessionId ->
                if (reportedReadySessionId != sessionId) {
                    reportedReadySessionId = sessionId
                    reportedErrorSignature = null
                    reportSessionFeedback("info", 200, "playing")
                }
            }
        }
        mutateState { current ->
            current.copy(
                playerState = playerState,
                playWhenReady = playWhenReady,
                lastError = error ?: current.lastError
            )
        }
    }

    private fun setState(value: NativePlaybackState) {
        stateStore.set(value)
    }

    private inline fun mutateState(transform: (NativePlaybackState) -> NativePlaybackState) {
        setState(transform(stateStore.current()))
    }

    private fun reportSessionFeedback(event: String, code: Int?, message: String?) {
        val sessionId = currentLiveSessionId() ?: return
        scope.launch {
            runCatching {
                playbackApi.reportPlaybackFeedback(sessionId, event, code, message)
            }.onFailure { err ->
                Log.w(TAG, "failed to report playback feedback", err)
            }
        }
    }

    private fun currentLiveSessionId(): String? {
        val sessionId = stateStore.current().session?.sessionId ?: return null
        return runCatching {
            UUID.fromString(sessionId)
            sessionId
        }.getOrNull()
    }

    private companion object {
        const val TAG = "Xg2gPlaybackRuntime"
    }
}
