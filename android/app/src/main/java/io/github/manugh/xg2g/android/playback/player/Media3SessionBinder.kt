package io.github.manugh.xg2g.android.playback.player

import okhttp3.OkHttpClient

internal data class PlaybackSessionBinding(
    val sessionId: String,
    val playbackDecisionToken: String?,
    val accessToken: String?,
    val dpopProof: String?,
    val profileId: String?,
    val isLive: Boolean = true
)

internal class Media3SessionBinder {
    fun createBoundOkHttpClient(
        baseClient: OkHttpClient,
        binding: PlaybackSessionBinding
    ): OkHttpClient {
        return baseClient.newBuilder()
            .addInterceptor { chain ->
                val original = chain.request()
                val builder = original.newBuilder()

                if (!binding.accessToken.isNullOrBlank()) {
                    builder.header("Authorization", "Bearer ${binding.accessToken}")
                }
                if (!binding.dpopProof.isNullOrBlank()) {
                    builder.header("DPoP", binding.dpopProof)
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
