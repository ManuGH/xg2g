package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.net.PlaybackApi
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

internal class HeartbeatManager(
    private val playbackApi: PlaybackApi,
    private val scope: CoroutineScope,
    private val dispatcher: CoroutineDispatcher = Dispatchers.IO
) {
    private var job: Job? = null

    fun start(
        sessionId: String,
        intervalSeconds: Int,
        onSessionUpdated: (SessionSnapshot) -> Unit,
        onError: (Throwable) -> Unit
    ) {
        stop()
        if (intervalSeconds <= 0) {
            return
        }

        job = scope.launch(dispatcher) {
            var nextDelayMs = intervalSeconds * 1000L
            var consecutiveFailures = 0
            while (isActive) {
                delay(nextDelayMs)
                try {
                    val snapshot = playbackApi.heartbeat(sessionId)
                    consecutiveFailures = 0
                    nextDelayMs = intervalSeconds * 1000L
                    onSessionUpdated(snapshot)
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Throwable) {
                    consecutiveFailures += 1
                    if (error.isTerminalHeartbeatFailure()) {
                        onError(error)
                        return@launch
                    }
                    nextDelayMs = retryDelayMs(consecutiveFailures, intervalSeconds)
                }
            }
        }
    }

    fun stop() {
        job?.cancel()
        job = null
    }

    private fun Throwable.isTerminalHeartbeatFailure(): Boolean =
        this is PlaybackHttpException && statusCode in TERMINAL_HTTP_STATUS_CODES

    private fun retryDelayMs(failureCount: Int, intervalSeconds: Int): Long {
        val exponent = (failureCount - 1).coerceIn(0, 4)
        val retryMs = RETRY_BASE_MS shl exponent
        return retryMs.coerceAtMost(intervalSeconds * 1000L)
    }

    private companion object {
        const val RETRY_BASE_MS = 1_000L
        val TERMINAL_HTTP_STATUS_CODES = setOf(401, 403, 404, 410)
    }
}
