package io.github.manugh.xg2g.android.bridge

import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import org.json.JSONObject

internal object HostBridgeContract {
    const val BRIDGE_NAME = "Xg2gHostBridge"
    const val PROTOCOL_VERSION = 1

    sealed interface Event {
        val name: String
        fun payload(): JSONObject

        fun toMessageJson(): String = JSONObject()
            .put("protocolVersion", PROTOCOL_VERSION)
            .put("type", "event")
            .put("event", name)
            .put("payload", payload())
            .toString()
    }

    data class HostMediaKey(
        val action: String,
        val timestampMs: Long = System.currentTimeMillis()
    ) : Event {
        override val name: String = "hostMediaKey"

        override fun payload(): JSONObject = JSONObject()
            .put("action", action)
            .put("ts", timestampMs)
    }

    data class NativePlaybackState(
        private val stateJson: String
    ) : Event {
        override val name: String = "nativePlaybackState"

        override fun payload(): JSONObject = JSONObject(stateJson)
    }

    sealed interface Command {
        data object Hello : Command
        data class SetPlaybackActive(val active: Boolean) : Command
        data object RequestInputFocus : Command
        data class StartNativePlayback(val request: NativePlaybackRequest) : Command
        data object StopNativePlayback : Command
    }

    fun parseCommand(messageJson: String): Command {
        val message = JSONObject(messageJson)
        require(message.optInt("protocolVersion", -1) == PROTOCOL_VERSION) {
            "Unsupported host bridge protocol"
        }

        return when (message.getString("type")) {
            "hello" -> Command.Hello
            "command" -> parseNamedCommand(
                name = message.getString("command"),
                payload = message.optJSONObject("payload") ?: JSONObject()
            )
            else -> error("Unsupported host bridge message type")
        }
    }

    fun snapshotJson(
        serializedHostCapabilities: String,
        serializedPlaybackCapabilities: String,
        nativePlaybackStateJson: String
    ): String = JSONObject()
        .put("protocolVersion", PROTOCOL_VERSION)
        .put("type", "snapshot")
        .put("host", JSONObject(serializedHostCapabilities))
        .put("playbackCapabilities", JSONObject(serializedPlaybackCapabilities))
        .put("nativePlaybackState", JSONObject(nativePlaybackStateJson))
        .toString()

    private fun parseNamedCommand(name: String, payload: JSONObject): Command = when (name) {
        "setPlaybackActive" -> Command.SetPlaybackActive(payload.getBoolean("active"))
        "requestInputFocus" -> Command.RequestInputFocus
        "startNativePlayback" -> Command.StartNativePlayback(
            PlaybackJsonCodec.requestFromJson(payload.getJSONObject("request").toString())
        )
        "stopNativePlayback" -> Command.StopNativePlayback
        else -> error("Unsupported host bridge command")
    }
}
