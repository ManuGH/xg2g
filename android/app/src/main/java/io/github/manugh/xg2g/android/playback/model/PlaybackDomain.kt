package io.github.manugh.xg2g.android.playback.model

@JvmInline
value class SessionState(val wireValue: String) {
    val isTerminal: Boolean
        get() = this == Failed || this == Stopped || this == Error

    override fun toString(): String = wireValue

    companion object {
        val Unknown = SessionState("UNKNOWN")
        val Ready = SessionState("READY")
        val Active = SessionState("ACTIVE")
        val Failed = SessionState("FAILED")
        val Stopped = SessionState("STOPPED")
        val Error = SessionState("ERROR")

        fun fromWireValue(value: String?): SessionState {
            val normalized = value
                ?.trim()
                ?.takeIf(String::isNotEmpty)
                ?.uppercase()
                ?: Unknown.wireValue

            return when (normalized) {
                Ready.wireValue -> Ready
                Active.wireValue -> Active
                Failed.wireValue -> Failed
                Stopped.wireValue -> Stopped
                Error.wireValue -> Error
                Unknown.wireValue -> Unknown
                else -> SessionState(normalized)
            }
        }
    }
}

@JvmInline
value class SessionMode(val wireValue: String) {
    override fun toString(): String = wireValue

    companion object {
        val Live = SessionMode("LIVE")
        val Recording = SessionMode("RECORDING")

        fun fromWireValue(value: String?): SessionMode? {
            val normalized = value
                ?.trim()
                ?.takeIf(String::isNotEmpty)
                ?.uppercase()
                ?: return null

            return when (normalized) {
                Live.wireValue -> Live
                Recording.wireValue -> Recording
                else -> SessionMode(normalized)
            }
        }
    }
}

@JvmInline
value class PlaybackMode(val wireValue: String) {
    override fun toString(): String = wireValue

    companion object {
        val NativeHls = PlaybackMode("native_hls")
        val HlsJs = PlaybackMode("hlsjs")
        val Transcode = PlaybackMode("transcode")
        val DirectMp4 = PlaybackMode("direct_mp4")

        fun fromWireValue(value: String?): PlaybackMode? {
            val normalized = value
                ?.trim()
                ?.takeIf(String::isNotEmpty)
                ?.lowercase()
                ?: return null

            return when (normalized) {
                NativeHls.wireValue -> NativeHls
                "hls", HlsJs.wireValue, "direct_stream" -> HlsJs
                Transcode.wireValue -> Transcode
                DirectMp4.wireValue -> DirectMp4
                else -> PlaybackMode(normalized)
            }
        }
    }
}
