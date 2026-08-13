package io.github.manugh.xg2g.android.playback.player

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
    private val dpopProvider: DPoPProvider? = null
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
                    if (dpopProvider != null) {
                        builder.header("Authorization", "DPoP ${binding.accessToken}")
                        val dynamicProof = dpopProvider.createProof(method, urlStr, binding.accessToken)
                        builder.header("DPoP", dynamicProof)
                    } else {
                        builder.header("Authorization", "Bearer ${binding.accessToken}")
                    }
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
