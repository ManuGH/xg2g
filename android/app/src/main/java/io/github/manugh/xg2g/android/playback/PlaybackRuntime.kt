package io.github.manugh.xg2g.android.playback

import android.content.Context
import android.os.SystemClock
import android.util.Log
import androidx.media3.common.Player
import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.playback.model.NativePlaybackDiagnostics
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.NativePlaybackState
import io.github.manugh.xg2g.android.playback.model.SessionMode
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.SessionState
import io.github.manugh.xg2g.android.transport.playback.PlaybackApiClient
import io.github.manugh.xg2g.android.transport.playback.PlaybackSessionBinding
import io.github.manugh.xg2g.android.playback.player.PlayerEventForwarder
import io.github.manugh.xg2g.android.playback.player.PlayerHolder
import io.github.manugh.xg2g.android.playback.session.HeartbeatManager
import io.github.manugh.xg2g.android.playback.session.LiveSessionCoordinator
import io.github.manugh.xg2g.android.transport.playback.PlaybackErrorMapper
import io.github.manugh.xg2g.android.playback.session.ReadinessPoller
import io.github.manugh.xg2g.android.playback.trace.ClientPlaybackTraceRecorder
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import java.util.UUID

internal class PlaybackRuntime(
    context: Context,
    private val stateStore: PlaybackStateStore
) : PlaybackSession {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val playbackApi = PlaybackApiClient(context.applicationContext)
    private val playbackTrace = ClientPlaybackTraceRecorder()
    private val playerHolder = PlayerHolder(context.applicationContext, playbackApi.playerMediaTransport, playbackTrace)
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
    private var playerEventForwarder = PlayerEventForwarder(playerHolder.player, ::onPlayerStateChanged)
    private var reportedReadySessionId: String? = null
    private var reportedErrorSignature: String? = null
    private var traceUploadJob: Job? = null
    private var traceUploadGeneration = 0L
    private var lastTraceUploadAtMs = 0L
    private var traceUploadNeeded = false

    /**
     * Set when playback has been abandoned for good. Kept separate from the transient error field
     * because the session heartbeat clears that one on every tick, which would silently wipe the
     * only thing telling the viewer that the stream is dead.
     */
    @Volatile
    private var terminalError: String? = null

    override val player: Player
        get() = playerHolder.player

    init {
        playerHolder.onPlaybackTraceAnomaly = ::schedulePlaybackTraceUpload
        // The MediaTek decoder on Fire TV can only be recovered by rebuilding the player;
        // re-wire the event forwarder and tell the UI/session to re-attach.
        playerHolder.onPlayerReplaced = { replacement ->
            playerEventForwarder.dispose()
            playerEventForwarder = PlayerEventForwarder(replacement, ::onPlayerStateChanged)
            mutateState { current ->
                current.copy(playerGeneration = current.playerGeneration + 1)
            }
        }
        playerHolder.onUnrecoverable = { reason ->
            Log.e(TAG, "playback abandoned after repeated decoder failures: $reason")
            terminalError = reason
            reportSessionFeedback("error", null, reason)
            mutateState { current -> current.copy(lastError = reason) }
        }
        playerHolder.onDegraded = { warning ->
            Log.w(TAG, "playback continues in degraded mode: $warning")
            reportSessionFeedback("warning", null, warning)
            mutateState { current ->
                current.copy(
                    lastError = terminalError,
                    playbackWarning = warning
                )
            }
        }
        playerHolder.onRecovered = {
            Log.i(TAG, "decoder recovery confirmed by a newly rendered frame")
            reportSessionFeedback("info", 200, "decoder_recovered")
            mutateState { current -> current.copy(lastError = terminalError) }
        }
    }
    override val state: StateFlow<NativePlaybackState> = stateStore.state

    override suspend fun start(request: NativePlaybackRequest) {
        stop(force = true)
        terminalError = null
        setState(NativePlaybackState(activeRequest = request))

        when (request) {
            is NativePlaybackRequest.Live -> {
                val snapshot = liveSessionCoordinator.start(request)
                val playbackUrl = playbackApi.resolvePlaybackUrl(
                    snapshot.playbackUrl ?: playbackApi.sessionPlaylistUrl(snapshot.sessionId)
                )
                playbackTrace.startPlayback()
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
        flushPendingPlaybackTrace()
        playbackTrace.endPlayback()
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
        cancelTraceUpload()
        playbackTrace.endPlayback()
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
                lastError = terminalError
            )
        }
    }

    private fun updateDiagnostics(diagnostics: NativePlaybackDiagnostics) {
        mutateState { current ->
            current.copy(
                diagnostics = diagnostics,
                lastError = terminalError
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

    private fun mutateState(transform: (NativePlaybackState) -> NativePlaybackState) {
        stateStore.update(transform)
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

    private fun schedulePlaybackTraceUpload() {
        val sessionId = currentLiveSessionId() ?: return
        traceUploadNeeded = true
        if (traceUploadJob?.isActive == true) return
        val now = SystemClock.elapsedRealtime()
        val cooldownMs = if (lastTraceUploadAtMs == 0L) 0L else (TRACE_UPLOAD_COOLDOWN_MS - (now - lastTraceUploadAtMs)).coerceAtLeast(0L)
        val waitMs = maxOf(TRACE_UPLOAD_TAIL_MS, cooldownMs)
        val generation = ++traceUploadGeneration
        traceUploadJob = scope.launch {
            var uploadSucceeded = false
            try {
                delay(waitMs)
                if (generation != traceUploadGeneration || currentLiveSessionId() != sessionId) return@launch
                val batch = playbackTrace.snapshot() ?: return@launch
                playbackApi.reportPlaybackTrace(sessionId, batch)
                uploadSucceeded = true
                traceUploadNeeded = playbackTrace.hasAnomalyAfter(batch.events.last().sequence)
                lastTraceUploadAtMs = SystemClock.elapsedRealtime()
                Log.i(TAG, "uploaded ${batch.events.size} playback trace events for session=$sessionId")
            } catch (cancelled: kotlinx.coroutines.CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                Log.w(TAG, "failed to upload playback trace for session=$sessionId", error)
            } finally {
                if (generation == traceUploadGeneration) {
                    traceUploadJob = null
                    if (uploadSucceeded && traceUploadNeeded) schedulePlaybackTraceUpload()
                }
            }
        }
    }

    private fun cancelTraceUpload() {
        traceUploadGeneration++
        traceUploadJob?.cancel()
        traceUploadJob = null
    }

    private fun flushPendingPlaybackTrace() {
        if (!traceUploadNeeded) {
            cancelTraceUpload()
            return
        }
        val sessionId = currentLiveSessionId()
        val batch = playbackTrace.snapshot()
        cancelTraceUpload()
        if (sessionId == null || batch == null) return
        traceUploadNeeded = false
        scope.launch {
            runCatching { playbackApi.reportPlaybackTrace(sessionId, batch) }
                .onSuccess { lastTraceUploadAtMs = SystemClock.elapsedRealtime() }
                .onFailure { error -> Log.w(TAG, "failed to flush playback trace for session=$sessionId", error) }
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
        const val TRACE_UPLOAD_TAIL_MS = 2_000L
        const val TRACE_UPLOAD_COOLDOWN_MS = 5_000L
    }
}
