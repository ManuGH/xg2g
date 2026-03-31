package io.github.manugh.xg2g.android.playback.bridge

import android.content.Context
import android.content.Intent
import io.github.manugh.xg2g.android.playback.PlayerContract
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.service.PlaybackService
import io.github.manugh.xg2g.android.playback.ui.PlayerActivity

class NativePlaybackBridge(
    private val context: Context
) {
    fun start(request: NativePlaybackRequest) {
        val appContext = context.applicationContext
        PlaybackSessionRegistry.getOrCreate(appContext)
        appContext.startService(
            Intent(appContext, PlaybackService::class.java)
                .setAction(PlayerContract.ACTION_START)
                .putExtra(PlayerContract.EXTRA_REQUEST_JSON, PlaybackJsonCodec.requestToJson(request))
        )

        val activityIntent = Intent(context, PlayerActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        if (context !is android.app.Activity) {
            activityIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        }
        context.startActivity(activityIntent)
    }

    fun stop() {
        val appContext = context.applicationContext
        appContext.startService(
            Intent(appContext, PlaybackService::class.java)
                .setAction(PlayerContract.ACTION_STOP)
        )
    }

    fun currentStateJson(): String = PlaybackSessionRegistry.currentStateJson()
}
