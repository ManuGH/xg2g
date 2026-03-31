package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.net.PlaybackApi
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

internal class HeartbeatManager(
    private val playbackApi: PlaybackApi,
    private val scope: CoroutineScope
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

        job = scope.launch(Dispatchers.IO) {
            while (isActive) {
                delay(intervalSeconds * 1000L)
                runCatching { playbackApi.heartbeat(sessionId) }
                    .onSuccess(onSessionUpdated)
                    .onFailure { error ->
                        onError(error)
                        cancel()
                    }
            }
        }
    }

    fun stop() {
        job?.cancel()
        job = null
    }
}
