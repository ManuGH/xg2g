package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.fail
import org.junit.Test

class ReadinessPollerTest {
    @Test
    fun `awaitRecordingPlayback retries until playback info and playlist become ready`() = runTest {
        val request = NativePlaybackRequest.Recording(recordingId = "rec-monk")
        val playbackApi = FakePlaybackApi(
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
        private val recordingInfoResults: MutableList<io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo?>,
        private val playbackUrlResults: MutableList<String?>,
        private val recordingPlaylistResults: MutableList<String?>
    ) : io.github.manugh.xg2g.android.playback.net.PlaybackApi {
        val recordingInfoChecks = mutableListOf<String>()
        val playbackUrlChecks = mutableListOf<String>()
        val recordingPlaylistChecks = mutableListOf<String>()

        override suspend fun ensureAuthSession(authToken: String?) = Unit

        override suspend fun startLiveIntent(request: NativePlaybackRequest.Live): NativeLiveStartResult {
            error("startLiveIntent should not run in these tests")
        }

        override suspend fun getSessionState(sessionId: String): SessionSnapshot {
            error("getSessionState should not run in these tests")
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
}
