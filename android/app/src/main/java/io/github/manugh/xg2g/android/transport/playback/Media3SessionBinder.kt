package io.github.manugh.xg2g.android.transport.playback

import androidx.media3.common.MediaItem
import androidx.media3.datasource.okhttp.OkHttpDataSource
import androidx.media3.exoplayer.source.DefaultMediaSourceFactory
import androidx.media3.exoplayer.source.MediaSource
import io.github.manugh.xg2g.android.auth.DPoPProvider
import okhttp3.OkHttpClient

internal data class PlaybackSessionBinding(
    val sessionId: String,
    val playbackDecisionToken: String?,
    val accessToken: String?,
    val profileId: String?,
    val isLive: Boolean = true
)

internal class Media3SessionBinder(
    private val dpopProvider: DPoPProvider
) {
    fun createBoundOkHttpClient(
        baseClient: OkHttpClient,
        binding: PlaybackSessionBinding
    ): OkHttpClient {
        return baseClient.newBuilder()
            .addInterceptor { chain ->
                val original = chain.request()
                val builder = original.newBuilder()

                val urlStr = original.url.toString()
                val method = original.method

                if (!binding.accessToken.isNullOrBlank()) {
                    builder.header("Authorization", "DPoP ${binding.accessToken}")
                    val dynamicProof = dpopProvider.createProof(method, urlStr, binding.accessToken)
                    builder.header("DPoP", dynamicProof)
                }
                if (!binding.profileId.isNullOrBlank()) {
                    builder.header("X-Household-Profile", binding.profileId)
                }
                if (!binding.playbackDecisionToken.isNullOrBlank()) {
                    builder.header("X-XG2G-Playback-Decision-Token", binding.playbackDecisionToken)
                }

                chain.proceed(builder.build())
            }
            .build()
    }
}

/**
 * What the player asks for: a media source for an item.
 *
 * The player used to assemble one itself — take an OkHttp client, bind the
 * session onto it, wrap it in a data source factory. Every one of those steps
 * is a network decision, so the player now states the intent and the transport
 * decides how it is carried.
 */
internal interface PlayerMediaTransport {
    fun createMediaSource(
        mediaItem: MediaItem,
        binding: PlaybackSessionBinding?,
        requestHeaders: Map<String, String>
    ): MediaSource
}

internal class Media3PlayerTransport(
    private val baseClient: OkHttpClient,
    dpopProvider: DPoPProvider
) : PlayerMediaTransport {
    private val sessionBinder = Media3SessionBinder(dpopProvider)

    override fun createMediaSource(
        mediaItem: MediaItem,
        binding: PlaybackSessionBinding?,
        requestHeaders: Map<String, String>
    ): MediaSource {
        val client = binding?.let { sessionBinder.createBoundOkHttpClient(baseClient, it) } ?: baseClient
        val dataSourceFactory = OkHttpDataSource.Factory(client)
        if (requestHeaders.isNotEmpty()) {
            dataSourceFactory.setDefaultRequestProperties(requestHeaders)
        }
        return DefaultMediaSourceFactory(dataSourceFactory).createMediaSource(mediaItem)
    }
}
