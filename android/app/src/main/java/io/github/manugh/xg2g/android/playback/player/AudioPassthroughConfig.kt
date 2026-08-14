package io.github.manugh.xg2g.android.playback.player

internal enum class AudioOutputMode {
    STEREO,
    PASSTHROUGH
}

internal class AudioPassthroughConfig {
    fun resolveAudioMode(modeString: String?): AudioOutputMode {
        return when (modeString?.lowercase()?.trim()) {
            "passthrough", "ac3", "raw", "surround" -> AudioOutputMode.PASSTHROUGH
            else -> AudioOutputMode.STEREO
        }
    }

    fun isPassthroughEnabled(modeString: String?): Boolean {
        return resolveAudioMode(modeString) == AudioOutputMode.PASSTHROUGH
    }
}
