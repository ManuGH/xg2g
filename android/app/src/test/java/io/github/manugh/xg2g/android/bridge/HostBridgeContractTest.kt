package io.github.manugh.xg2g.android.bridge

import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Test

class HostBridgeContractTest {
    @Test
    fun `start native playback command parses request payload`() {
        val command = HostBridgeContract.parseCommand(
            """
                {
                  "protocolVersion": 1,
                  "type": "command",
                  "command": "startNativePlayback",
                  "payload": {
                    "request": {
                      "kind": "live",
                      "serviceRef": "1:0:1:AA",
                      "playbackDecisionToken": "token-123",
                      "authToken": "dev-token",
                      "title": "Das Erste HD",
                      "params": {
                        "playback_mode": "native_hls"
                      }
                    }
                  }
                }
            """.trimIndent()
        ) as HostBridgeContract.Command.StartNativePlayback

        val request = command.request as NativePlaybackRequest.Live
        assertEquals("1:0:1:AA", request.serviceRef)
        assertEquals("token-123", request.playbackDecisionToken)
        assertEquals("dev-token", request.authToken)
        assertEquals("Das Erste HD", request.title)
        assertEquals("native_hls", request.params["playback_mode"])
    }

    @Test
    fun `snapshot contains all initial bridge state`() {
        val snapshot = JSONObject(
            HostBridgeContract.snapshotJson(
                serializedHostCapabilities = """{"platform":"android-tv"}""",
                serializedPlaybackCapabilities = """{"deviceType":"android_tv"}""",
                nativePlaybackStateJson = """{"playerState":3}"""
            )
        )

        assertEquals(HostBridgeContract.PROTOCOL_VERSION, snapshot.getInt("protocolVersion"))
        assertEquals("snapshot", snapshot.getString("type"))
        assertEquals("android-tv", snapshot.getJSONObject("host").getString("platform"))
        assertEquals("android_tv", snapshot.getJSONObject("playbackCapabilities").getString("deviceType"))
        assertEquals(3, snapshot.getJSONObject("nativePlaybackState").getInt("playerState"))
    }

    @Test
    fun `native playback state uses structured event message`() {
        val message = JSONObject(
            HostBridgeContract.NativePlaybackState("""{"playerState":3}""").toMessageJson()
        )

        assertEquals("event", message.getString("type"))
        assertEquals("nativePlaybackState", message.getString("event"))
        assertEquals(3, message.getJSONObject("payload").getInt("playerState"))
    }
}
