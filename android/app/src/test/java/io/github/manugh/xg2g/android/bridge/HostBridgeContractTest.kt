package io.github.manugh.xg2g.android.bridge

import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HostBridgeContractTest {
    @Test
    fun `start native playback command parses request payload`() {
        val command = HostBridgeContract.Command.StartNativePlayback.parse(
            """
                {
                  "kind": "live",
                  "serviceRef": "1:0:1:AA",
                  "playbackDecisionToken": "token-123",
                  "authToken": "dev-token",
                  "title": "Das Erste HD",
                  "params": {
                    "playback_mode": "native_hls"
                  }
                }
            """.trimIndent()
        )

        val request = command.request as NativePlaybackRequest.Live
        assertEquals("1:0:1:AA", request.serviceRef)
        assertEquals("token-123", request.playbackDecisionToken)
        assertEquals("dev-token", request.authToken)
        assertEquals("Das Erste HD", request.title)
        assertEquals("native_hls", request.params["playback_mode"])
    }

    @Test
    fun `host ready event script sets host object and dispatches ready event`() {
        val script = HostBridgeContract.HostReady("""{"platform":"android"}""").toJavascript()

        assertTrue(script.contains("window.${HostBridgeContract.HOST_OBJECT_NAME} = detail;"))
        assertTrue(script.contains(HostBridgeContract.HOST_READY_EVENT))
    }

    @Test
    fun `native playback state event script dispatches shared event name`() {
        val script = HostBridgeContract.NativePlaybackState("""{"playerState":3}""").toJavascript()

        assertTrue(script.contains(HostBridgeContract.NATIVE_PLAYBACK_STATE_EVENT))
        assertTrue(script.contains("JSON.parse"))
    }
}
