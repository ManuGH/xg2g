package io.github.manugh.xg2g.android.bridge

import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import org.json.JSONObject

internal object HostBridgeContract {
    const val BRIDGE_NAME = "Xg2gHost"
    const val HOST_OBJECT_NAME = "__XG2G_HOST__"
    const val HOST_READY_EVENT = "xg2g:host-ready"
    const val HOST_MEDIA_KEY_EVENT = "xg2g:host-media-key"
    const val NATIVE_PLAYBACK_STATE_EVENT = "xg2g:native-playback-state"

    sealed interface Event {
        val eventName: String

        fun detailJson(): String

        fun beforeDispatch(detailRef: String): String = ""

        fun toJavascript(): String {
            val escapedDetailJson = JSONObject.quote(detailJson())
            val beforeDispatch = beforeDispatch("detail")

            return buildString {
                appendLine("(() => {")
                appendLine("  try {")
                appendLine("    const detail = JSON.parse($escapedDetailJson);")
                if (beforeDispatch.isNotBlank()) {
                    appendLine("    $beforeDispatch")
                }
                appendLine("    window.dispatchEvent(new CustomEvent('$eventName', { detail }));")
                appendLine("  } catch (_) {")
                appendLine("  }")
                append("})();")
            }
        }
    }

    data class HostReady(
        private val serializedHostCapabilities: String
    ) : Event {
        override val eventName: String = HOST_READY_EVENT

        override fun detailJson(): String = serializedHostCapabilities

        override fun beforeDispatch(detailRef: String): String =
            "window.$HOST_OBJECT_NAME = $detailRef;"
    }

    data class HostMediaKey(
        val action: String,
        val timestampMs: Long = System.currentTimeMillis()
    ) : Event {
        override val eventName: String = HOST_MEDIA_KEY_EVENT

        override fun detailJson(): String = JSONObject()
            .put("action", action)
            .put("ts", timestampMs)
            .toString()
    }

    data class NativePlaybackState(
        private val stateJson: String
    ) : Event {
        override val eventName: String = NATIVE_PLAYBACK_STATE_EVENT

        override fun detailJson(): String = stateJson
    }

    sealed interface Command {
        data class SetPlaybackActive(val active: Boolean) : Command
        data object RequestInputFocus : Command
        data class StartNativePlayback(val request: NativePlaybackRequest) : Command {
            companion object {
                fun parse(requestJson: String): StartNativePlayback {
                    return StartNativePlayback(PlaybackJsonCodec.requestFromJson(requestJson))
                }
            }
        }

        data object StopNativePlayback : Command
    }
}
