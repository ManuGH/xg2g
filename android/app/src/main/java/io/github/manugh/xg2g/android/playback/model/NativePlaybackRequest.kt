




package io.github.manugh.xg2g.android.playback.model

sealed interface NativePlaybackRequest {
    val title: String?
    val logoUrl: String?
    val correlationId: String?
    val authToken: String?

    data class Live(
        val serviceRef: String,
        val playbackDecisionToken: String? = null,
        val hwaccel: String? = null,
        val params: Map<String, String> = emptyMap(),
        override val title: String? = null,
        override val logoUrl: String? = null,
        override val correlationId: String? = null,
        override val authToken: String? = null
    ) : NativePlaybackRequest

    data class Recording(
        val recordingId: String,
        val startPositionMs: Long = 0L,
        override val title: String? = null,
        override val logoUrl: String? = null,
        override val correlationId: String? = null,
        override val authToken: String? = null
    ) : NativePlaybackRequest
}
