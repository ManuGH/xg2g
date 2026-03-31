package io.github.manugh.xg2g.android.playback.player

import android.content.Context
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.DefaultRenderersFactory
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.datasource.okhttp.OkHttpDataSource
import okhttp3.OkHttpClient

internal class PlayerHolder(
    context: Context,
    private val okHttpClient: OkHttpClient
) {
    private val renderersFactory = DefaultRenderersFactory(context.applicationContext)
        .setEnableAudioTrackPlaybackParams(false)
        .setEnableAudioOutputPlaybackParameters(false)

    val player: ExoPlayer = ExoPlayer.Builder(context.applicationContext, renderersFactory)
        .build()
        .apply {
            setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(C.USAGE_MEDIA)
                    .setContentType(C.AUDIO_CONTENT_TYPE_MOVIE)
                    .build(),
                true
            )
            playWhenReady = true
        }

    fun playUrl(
        url: String,
        mediaId: String,
        title: String?,
        isLive: Boolean,
        requestHeaders: Map<String, String> = emptyMap(),
        mimeType: String? = null,
        startPositionMs: Long = 0L
    ) {
        val mediaItemBuilder = MediaItem.Builder()
            .setUri(url)
            .setMediaId(mediaId)
            .setMediaMetadata(
                MediaMetadata.Builder()
                    .setTitle(title ?: mediaId)
                    .build()
            )
        if (!mimeType.isNullOrBlank()) {
            mediaItemBuilder.setMimeType(mimeType)
        }

        if (isLive) {
            mediaItemBuilder.setLiveConfiguration(
                MediaItem.LiveConfiguration.Builder().build()
            )
        }

        val mediaItem = mediaItemBuilder.build()
        val dataSourceFactory = OkHttpDataSource.Factory(okHttpClient)
        if (requestHeaders.isNotEmpty()) {
            dataSourceFactory.setDefaultRequestProperties(requestHeaders)
        }
        val mediaSource = DefaultMediaSourceFactory(dataSourceFactory)
            .createMediaSource(mediaItem)

        player.setMediaSource(mediaSource, startPositionMs)
        player.prepare()
        player.playWhenReady = true
    }

    fun clear() {
        player.stop()
        player.clearMediaItems()
    }

    fun release() {
        player.release()
    }
}
