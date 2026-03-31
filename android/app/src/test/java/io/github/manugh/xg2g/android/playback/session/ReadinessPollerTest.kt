package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.SessionMode
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.SessionState
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class ReadinessPollerTest {
    @Test
    fun `awaitReady returns as soon as playbackUrl exists`() = runTest {
        val playbackApi = FakePlaybackApi(
            sessionResults = mutableListOf(
                sessionSnapshot(state = SessionState.Unknown, playbackUrl = "/api/v3/sessions/sess-live-1/hls/index.m3u8"),
                sessionSnapshot(state = SessionState.Ready, playbackUrl = "/api/v3/sessions/sess-live-1/hls/index.m3u8")
            ),
            recordingInfoResults = mutableListOf(),
            playbackUrlResults = mutableListOf(),
            recordingPlaylistResults = mutableListOf()
        )
        val poller = ReadinessPoller(playbackApi, PlaybackErrorMapper())

        val snapshot = poller.awaitReady(
            sessionId = "sess-live-1",
            maxAttempts = 2,
            pollMs = 0L
        )

        assertEquals(SessionState.Unknown, snapshot.state)
        assertEquals(listOf("sess-live-1"), playbackApi.sessionChecks)
    }

    @Test
    fun `awaitReady fails when terminal state arrives before READY`() = runTest {
        val playbackApi = FakePlaybackApi(
            sessionResults = mutableListOf(
                sessionSnapshot(state = SessionState.Failed, playbackUrl = null)
            ),
            recordingInfoResults = mutableListOf(),
            playbackUrlResults = mutableListOf(),
            recordingPlaylistResults = mutableListOf()
        )
        val poller = ReadinessPoller(playbackApi, PlaybackErrorMapper())

        val error = try {
            poller.awaitReady(
                sessionId = "sess-live-1",
                maxAttempts = 1,
                pollMs = 0L
            )
            fail("Expected awaitReady to throw")
            throw AssertionError("unreachable")
        } catch (thrown: IllegalStateException) {
            thrown
        }

        assertTrue(error.message.orEmpty().contains("terminal state FAILED"))
        assertEquals(listOf("sess-live-1"), playbackApi.sessionChecks)
    }

    @Test
    fun `awaitRecordingPlayback retries until playback info and playlist become ready`() = runTest {
        val request = NativePlaybackRequest.Recording(recordingId = "rec-monk")
        val playbackApi = FakePlaybackApi(
            sessionResults = mutableListOf(),
            recordingInfoResults = mutableListOf(
                null,
                io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo("/api/v3/recordings/rec-monk/playlist.m3u8")
            ),
            playbackUrlResults = mutableListOf(
                null,
                "http://example.invalid/recording/monk.m3u8"
            ),
            recordingPlaylistResults = mutableListOf()
        )
        val poller = ReadinessPoller(playbackApi, PlaybackErrorMapper())

        val playback = poller.awaitRecordingPlayback(
            request = request,
            maxAttempts = 3,
            pollMs = 0L
        )

        assertEquals("http://example.invalid/recording/monk.m3u8", playback.playbackUrl)
        assertEquals(listOf("rec-monk", "rec-monk"), playbackApi.recordingInfoChecks)
        assertEquals(
            listOf("/api/v3/recordings/rec-monk/playlist.m3u8", "/api/v3/recordings/rec-monk/playlist.m3u8"),
            playbackApi.playbackUrlChecks
        )
    }

    @Test
    fun `awaitRecordingPlaylist retries until playlist becomes ready`() = runTest {
        val playbackApi = FakePlaybackApi(
            sessionResults = mutableListOf(),
            recordingInfoResults = mutableListOf(),
            playbackUrlResults = mutableListOf(),
            recordingPlaylistResults = mutableListOf(null, null, "http://example.invalid/recording/monk.m3u8")
        )
        val poller = ReadinessPoller(playbackApi, PlaybackErrorMapper())

        val playlistUrl = poller.awaitRecordingPlaylist(
            recordingId = "rec-monk",
            maxAttempts = 3,
            pollMs = 0L
        )

        assertEquals("http://example.invalid/recording/monk.m3u8", playlistUrl)
        assertEquals(listOf("rec-monk", "rec-monk", "rec-monk"), playbackApi.recordingPlaylistChecks)
    }

    @Test
    fun `awaitRecordingPlaylist fails after exhausting retries`() = runTest {
        val playbackApi = FakePlaybackApi(
            sessionResults = mutableListOf(),
            recordingInfoResults = mutableListOf(),
            playbackUrlResults = mutableListOf(),
            recordingPlaylistResults = mutableListOf(null, null)
        )
        val poller = ReadinessPoller(playbackApi, PlaybackErrorMapper())

        val error = try {
            poller.awaitRecordingPlaylist(
                recordingId = "rec-monk",
                maxAttempts = 2,
                pollMs = 0L
            )
            fail("Expected awaitRecordingPlaylist to throw")
            throw AssertionError("unreachable")
        } catch (thrown: IllegalStateException) {
            thrown
        }

        assertEquals(
            "Recording rec-monk playlist did not become ready in 0ms",
            error.message
        )
        assertEquals(listOf("rec-monk", "rec-monk"), playbackApi.recordingPlaylistChecks)
    }

    private class FakePlaybackApi(
        private val sessionResults: MutableList<SessionSnapshot>,
        private val recordingInfoResults: MutableList<io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo?>,
        private val playbackUrlResults: MutableList<String?>,
        private val recordingPlaylistResults: MutableList<String?>
    ) : io.github.manugh.xg2g.android.playback.net.PlaybackApi {
        val sessionChecks = mutableListOf<String>()
        val recordingInfoChecks = mutableListOf<String>()
        val playbackUrlChecks = mutableListOf<String>()
        val recordingPlaylistChecks = mutableListOf<String>()

        override suspend fun ensureAuthSession(authToken: String?) = Unit

        override suspend fun startLiveIntent(request: NativePlaybackRequest.Live): NativeLiveStartResult {
            error("startLiveIntent should not run in these tests")
        }

        override suspend fun getSessionState(sessionId: String): SessionSnapshot {
            sessionChecks += sessionId
            return if (sessionResults.isEmpty()) sessionSnapshot(state = SessionState.Unknown, playbackUrl = null, sessionId = sessionId)
            else sessionResults.removeAt(0)
        }

        override suspend fun getRecordingPlaybackInfo(request: NativePlaybackRequest.Recording): io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo? {
            recordingInfoChecks += request.recordingId
            return if (recordingInfoResults.isEmpty()) null else recordingInfoResults.removeAt(0)
        }

        override suspend fun getRecordingPlaylistIfReady(recordingId: String): String? {
            recordingPlaylistChecks += recordingId
            return if (recordingPlaylistResults.isEmpty()) null else recordingPlaylistResults.removeAt(0)
        }

        override suspend fun getPlaybackUrlIfReady(playbackUrl: String): String? {
            playbackUrlChecks += playbackUrl
            return if (playbackUrlResults.isEmpty()) null else playbackUrlResults.removeAt(0)
        }

        override suspend fun heartbeat(sessionId: String): SessionSnapshot {
            error("heartbeat should not run in these tests")
        }

        override suspend fun reportPlaybackFeedback(sessionId: String, event: String, code: Int?, message: String?) = Unit

        override suspend fun stopSession(sessionId: String) {
            error("stopSession should not run in these tests")
        }

        override fun sessionPlaylistUrl(sessionId: String): String {
            error("sessionPlaylistUrl should not run in these tests")
        }

        override fun recordingPlaylistUrl(recordingId: String): String {
            return "http://example.invalid/recording/$recordingId.m3u8"
        }
    }

    private companion object {
        fun sessionSnapshot(
            state: SessionState,
            playbackUrl: String?,
            sessionId: String = "sess-live-1"
        ) = SessionSnapshot(
            sessionId = sessionId,
            state = state,
            playbackUrl = playbackUrl,
            mode = SessionMode.Live,
            requestId = "req-1",
            profileReason = null,
            traceJson = null,
            heartbeatIntervalSec = null,
            leaseExpiresAt = null,
            durationSeconds = null,
            seekableStartSeconds = null,
            seekableEndSeconds = null,
            liveEdgeSeconds = null
        )
    }
}
