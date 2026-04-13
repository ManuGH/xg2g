package io.github.manugh.xg2g.android.playback.service

import android.content.Intent
import androidx.media3.session.MediaSession
import androidx.media3.session.MediaSessionService
import io.github.manugh.xg2g.android.playback.PlayerContract
import io.github.manugh.xg2g.android.playback.PlaybackSession
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch

class PlaybackService : MediaSessionService() {
    private val serviceScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private lateinit var runtime: PlaybackSession
    private lateinit var mediaSession: MediaSession
    private var commandJob: Job? = null

    override fun onCreate() {
        super.onCreate()
        runtime = PlaybackSessionRegistry.getOrCreate(this)
        mediaSession = MediaSession.Builder(this, runtime.player)
            .setCallback(PlaybackMediaSessionCallback())
            .build()
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaSession = mediaSession

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val superStartMode = super.onStartCommand(intent, flags, startId)
        when (intent?.action) {
            PlayerContract.ACTION_START -> {
                val requestJson = intent.getStringExtra(PlayerContract.EXTRA_REQUEST_JSON) ?: return START_NOT_STICKY
                launchCommand {
                    val request = PlaybackJsonCodec.requestFromJson(requestJson)
                    runtime.start(request)
                }
            }

            PlayerContract.ACTION_STOP -> {
                launchCommand {
                    runtime.stop(force = true)
                    stopSelf()
                }
            }

            else -> return superStartMode
        }
        return START_STICKY
    }

    override fun onDestroy() {
        commandJob?.cancel()
        mediaSession.release()
        serviceScope.cancel()
        PlaybackSessionRegistry.release()
        super.onDestroy()
    }

    private fun launchCommand(block: suspend () -> Unit) {
        val previousJob = commandJob
        commandJob = serviceScope.launch {
            previousJob?.cancelAndJoin()
            runCatching { block() }
                .onFailure { error ->
                    if (error is CancellationException) {
                        return@onFailure
                    }
                    runtime.reportCommandFailure(error)
                }
        }
    }
}
