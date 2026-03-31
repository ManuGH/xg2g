package io.github.manugh.xg2g.android.playback.net

import androidx.media3.common.MimeTypes
import okhttp3.Request
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.RequestBody.Companion.toRequestBody
import org.junit.Assert.assertEquals
import org.junit.Test

class PlaybackApiClientHeadersTest {
    @Test
    fun `same origin headers use ui base origin and referer`() {
        val request = Request.Builder()
            .url("http://127.0.0.1:8080/api/v3/auth/session")
            .post(ByteArray(0).toRequestBody(null))
            .build()

        val contextualRequest = request.withSameOriginHeaders("http://127.0.0.1:8080/ui/".toHttpUrl())

        assertEquals("http://127.0.0.1:8080", contextualRequest.header("Origin"))
        assertEquals("http://127.0.0.1:8080/ui/", contextualRequest.header("Referer"))
    }

    @Test
    fun `same origin headers preserve explicit request headers`() {
        val request = Request.Builder()
            .url("https://xg2g.example.invalid/api/v3/intents")
            .header("Origin", "https://custom.example.invalid")
            .header("Referer", "https://custom.example.invalid/ui/")
            .post(ByteArray(0).toRequestBody(null))
            .build()

        val contextualRequest = request.withSameOriginHeaders("https://xg2g.example.invalid/ui/".toHttpUrl())

        assertEquals("https://custom.example.invalid", contextualRequest.header("Origin"))
        assertEquals("https://custom.example.invalid/ui/", contextualRequest.header("Referer"))
    }

    @Test
    fun `origin header omits default https port`() {
        assertEquals(
            "https://xg2g.example.invalid",
            "https://xg2g.example.invalid:443/ui/".toHttpUrl().originHeaderValue()
        )
    }

    @Test
    fun `relative playback urls resolve against ui base host`() {
        assertEquals(
            "http://127.0.0.1:8080/api/v3/sessions/abc/hls/index.m3u8",
            "http://127.0.0.1:8080/ui/".toHttpUrl()
                .resolveAgainst("/api/v3/sessions/abc/hls/index.m3u8")
        )
    }

    @Test
    fun `recording playlist url uses android native profile query`() {
        assertEquals(
            "http://127.0.0.1:8080/api/v3/recordings/rec-monk/playlist.m3u8?profile=android_native",
            recordingPlaylistHttpUrl(
                apiUrl = "http://127.0.0.1:8080/api/v3/recordings/rec-monk/playlist.m3u8".toHttpUrl(),
                profile = "android_native"
            ).toString()
        )
    }

    @Test
    fun `native recording playback preserves direct file decision`() {
        assertEquals(
            "/api/v3/recordings/rec-monk/stream.mp4",
            normalizeRecordingPlaybackUrl(
                uiBaseUrl = "http://127.0.0.1:8080/ui/".toHttpUrl(),
                recordingId = "rec-monk",
                playbackUrl = "/api/v3/recordings/rec-monk/stream.mp4",
                profile = "android_native",
                selectedOutputKind = "file"
            )
        )
    }

    @Test
    fun `native recording playback preserves direct play stream decision without explicit file output kind`() {
        assertEquals(
            "/api/v3/recordings/rec-monk/stream.mp4",
            normalizeRecordingPlaybackUrl(
                uiBaseUrl = "http://127.0.0.1:8080/ui/".toHttpUrl(),
                recordingId = "rec-monk",
                playbackUrl = "/api/v3/recordings/rec-monk/stream.mp4",
                profile = "android_native",
                decisionMode = "direct_play"
            )
        )
    }

    @Test
    fun `native recording playback rewrites non file stream decision to android playlist`() {
        assertEquals(
            "http://127.0.0.1:8080/api/v3/recordings/rec-monk/playlist.m3u8?profile=android_native",
            normalizeRecordingPlaybackUrl(
                uiBaseUrl = "http://127.0.0.1:8080/ui/".toHttpUrl(),
                recordingId = "rec-monk",
                playbackUrl = "/api/v3/recordings/rec-monk/stream.mp4",
                profile = "android_native",
                selectedOutputKind = "hls"
            )
        )
    }

    @Test
    fun `native recording playback preserves hls playlist decision`() {
        assertEquals(
            "/api/v3/recordings/rec-monk/playlist.m3u8?profile=android_native",
            normalizeRecordingPlaybackUrl(
                uiBaseUrl = "http://127.0.0.1:8080/ui/".toHttpUrl(),
                recordingId = "rec-monk",
                playbackUrl = "/api/v3/recordings/rec-monk/playlist.m3u8?profile=android_native",
                profile = "android_native"
            )
        )
    }

    @Test
    fun `recording playback mime type maps direct transport stream`() {
        assertEquals("file", NativeRecordingPlaybackInfo("/stream.mp4", selectedOutputKind = "file").selectedOutputKind)
        assertEquals(MimeTypes.VIDEO_MP2T, recordingPlaybackMimeType("file", "mpegts"))
    }

    @Test
    fun `recording playback mime type maps direct play transport stream without selected output kind`() {
        assertEquals(MimeTypes.VIDEO_MP2T, recordingPlaybackMimeType(null, "mpegts", "direct_play"))
    }

    @Test
    fun `playback request headers include same origin context and cookie`() {
        val headers = playbackRequestHeaders(
            uiBaseUrl = "http://127.0.0.1:8080/ui/".toHttpUrl(),
            cookieHeader = "xg2g_session=abc123; Path=/; HttpOnly"
        )

        assertEquals("http://127.0.0.1:8080", headers["Origin"])
        assertEquals("http://127.0.0.1:8080/ui/", headers["Referer"])
        assertEquals("xg2g_session=abc123; Path=/; HttpOnly", headers["Cookie"])
    }

    @Test
    fun `playback request headers omit cookie when unavailable`() {
        val headers = playbackRequestHeaders(
            uiBaseUrl = "http://127.0.0.1:8080/ui/".toHttpUrl(),
            cookieHeader = null
        )

        assertEquals("http://127.0.0.1:8080", headers["Origin"])
        assertEquals("http://127.0.0.1:8080/ui/", headers["Referer"])
        assertEquals(null, headers["Cookie"])
    }
}
