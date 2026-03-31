package io.github.manugh.xg2g.android.playback.net

import androidx.media3.common.MimeTypes
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PlaybackApiJsonCodecTest {
    @Test
    fun `live decision request encodes capability lists as json arrays`() {
        val body = PlaybackApiJsonCodec.liveDecisionRequestBody(
            request = NativePlaybackRequest.Live(
                serviceRef = "1:0:1:AA",
                title = "ORF1 HD",
                playbackDecisionToken = null,
                correlationId = null,
                hwaccel = null,
                authToken = "dev-token",
                params = emptyMap()
            ),
            capabilities = NativePlaybackCapabilities(
                capabilitiesVersion = 3,
                container = listOf("hls", "fmp4", "ts", "mp4"),
                videoCodecs = listOf("h264"),
                audioCodecs = listOf("aac", "ac3"),
                supportsHls = true,
                supportsRange = true,
                deviceType = "android_tv",
                hlsEngines = listOf("native"),
                preferredHlsEngine = "native",
                runtimeProbeUsed = false,
                runtimeProbeVersion = 1,
                clientFamilyFallback = "android_tv_native",
                allowTranscode = true,
                deviceContext = NativePlaybackDeviceContext(
                    brand = "google",
                    product = "mdarcy",
                    device = "foster",
                    platform = "android-tv",
                    manufacturer = "NVIDIA",
                    model = "Shield TV Pro",
                    osName = "android",
                    osVersion = "14",
                    sdkInt = 34,
                )
            )
        )

        val json = JSONObject(body)
        val capabilities = json.getJSONObject("capabilities")

        assertEquals(4, capabilities.getJSONArray("container").length())
        assertEquals("hls", capabilities.getJSONArray("container").getString(0))
        assertEquals("fmp4", capabilities.getJSONArray("container").getString(1))
        assertEquals("ts", capabilities.getJSONArray("container").getString(2))
        assertEquals("mp4", capabilities.getJSONArray("container").getString(3))
        assertEquals("h264", capabilities.getJSONArray("videoCodecs").getString(0))
        assertEquals("aac", capabilities.getJSONArray("audioCodecs").getString(0))
        assertEquals("native", capabilities.getJSONArray("hlsEngines").getString(0))
        val deviceContext = capabilities.getJSONObject("deviceContext")
        assertEquals("google", deviceContext.getString("brand"))
        assertEquals("mdarcy", deviceContext.getString("product"))
        assertEquals("foster", deviceContext.getString("device"))
        assertEquals("Shield TV Pro", deviceContext.getString("model"))
    }

    @Test
    fun `recording playback info parses direct play transport stream mime type`() {
        val parsed = PlaybackApiJsonCodec.parseRecordingPlaybackInfoResponse(
            JSONObject(
                """
                {
                  "requestId":"req-1",
                  "decision":{
                    "mode":"direct_play",
                    "selectedOutputUrl":"/api/v3/recordings/rec-7/stream.mp4",
                    "selectedOutputKind":"file",
                    "selected":{
                      "container":"mpegts"
                    }
                  }
                }
                """.trimIndent()
            )
        )

        assertEquals("/api/v3/recordings/rec-7/stream.mp4", parsed.playbackUrl)
        assertEquals("direct_play", parsed.decisionMode)
        assertEquals("file", parsed.selectedOutputKind)
        assertEquals(MimeTypes.VIDEO_MP2T, parsed.mimeType)
    }

    @Test
    fun `recording playback info does not infer file mime type for hls outputs`() {
        val parsed = PlaybackApiJsonCodec.parseRecordingPlaybackInfoResponse(
            JSONObject(
                """
                {
                  "decision":{
                    "mode":"direct_stream",
                    "selectedOutputUrl":"/api/v3/recordings/rec-7/playlist.m3u8",
                    "selectedOutputKind":"hls",
                    "selected":{
                      "container":"mpegts"
                    }
                  }
                }
                """.trimIndent()
            )
        )

        assertEquals("direct_stream", parsed.decisionMode)
        assertEquals("hls", parsed.selectedOutputKind)
        assertNull(parsed.mimeType)
    }
}
