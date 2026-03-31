package io.github.manugh.xg2g.android.playback.model

import androidx.media3.common.Player
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class PlaybackJsonCodecTest {
    @Test
    fun `request codec round-trips live request`() {
        val request = NativePlaybackRequest.Live(
            serviceRef = "1:0:1:AA",
            playbackDecisionToken = "decision-123",
            hwaccel = "auto",
            params = mapOf("playback_mode" to "native_hls"),
            title = "Das Erste HD",
            correlationId = "corr-1",
            authToken = "dev-token"
        )

        val parsed = PlaybackJsonCodec.requestFromJson(PlaybackJsonCodec.requestToJson(request))

        assertEquals(request, parsed)
    }

    @Test
    fun `state codec emits typed session and diagnostics fields with wire values`() {
        val stateJson = PlaybackJsonCodec.stateToJson(
            NativePlaybackState(
                activeRequest = NativePlaybackRequest.Recording(
                    recordingId = "rec-7",
                    startPositionMs = 42L,
                    title = "Tatort"
                ),
                session = SessionSnapshot(
                    sessionId = "sess-1",
                    state = SessionState.Ready,
                    playbackUrl = "https://example.invalid/hls.m3u8",
                    mode = SessionMode.Recording,
                    requestId = "req-1",
                    profileReason = "fallback",
                    traceJson = """{"stage":"ready"}""",
                    heartbeatIntervalSec = 5,
                    leaseExpiresAt = "2026-03-31T09:00:00Z",
                    durationSeconds = 120.0,
                    seekableStartSeconds = 0.0,
                    seekableEndSeconds = 120.0,
                    liveEdgeSeconds = null
                ),
                diagnostics = NativePlaybackDiagnostics(
                    requestId = "req-1",
                    playbackMode = PlaybackMode.NativeHls,
                    profileReason = "fallback",
                    capHash = "cap-1",
                    playbackInfoJson = """{"engine":"native"}""",
                    traceJson = """{"stage":"decision"}"""
                ),
                playerState = Player.STATE_READY,
                playWhenReady = true,
                isInPip = false,
                lastError = null
            )
        )

        val root = JSONObject(stateJson)
        assertEquals("recording", root.getJSONObject("activeRequest").getString("kind"))
        assertTrue(!root.getJSONObject("activeRequest").has("authToken"))
        assertEquals("READY", root.getJSONObject("session").getString("state"))
        assertEquals("RECORDING", root.getJSONObject("session").getString("mode"))
        assertEquals("native_hls", root.getJSONObject("diagnostics").getString("playbackMode"))
        assertTrue(root.getJSONObject("diagnostics").has("playbackInfo"))
    }
}
