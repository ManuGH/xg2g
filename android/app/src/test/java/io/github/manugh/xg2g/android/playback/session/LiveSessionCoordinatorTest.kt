package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.SessionMode
import io.github.manugh.xg2g.android.playback.model.SessionState
import io.github.manugh.xg2g.android.playback.net.PlaybackApi
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.fail
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class LiveSessionCoordinatorTest {
    @Test
    fun start_cancellation_stops_started_session() = runTest {
        val playbackApi = FakePlaybackApi(
            startResult = NativeLiveStartResult(sessionId = SESSION_ID, diagnostics = null),
            getSessionState = { awaitCancellation() }
        )
        val liveSessionCoordinator = createLiveSessionCoordinator(playbackApi)

        val job = launch {
            liveSessionCoordinator.start(TEST_REQUEST)
        }

        advanceUntilIdle()
        job.cancelAndJoin()

        assertEquals(listOf(SESSION_ID), playbackApi.stoppedSessions)
    }

    @Test
    fun start_failure_after_session_creation_stops_started_session() = runTest {
        val playbackApi = FakePlaybackApi(
            startResult = NativeLiveStartResult(sessionId = SESSION_ID, diagnostics = null),
            getSessionState = {
                SessionSnapshot(
                    sessionId = SESSION_ID,
                    state = SessionState.Failed,
                    playbackUrl = null,
                    mode = SessionMode.Live,
                    requestId = "req-failed",
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
        )
        val reportedErrors = mutableListOf<Throwable>()
        val liveSessionCoordinator = createLiveSessionCoordinator(playbackApi, onError = reportedErrors::add)

        val error: IllegalStateException = try {
            liveSessionCoordinator.start(TEST_REQUEST)
            fail("Expected liveSessionCoordinator.start to throw")
            throw AssertionError("unreachable")
        } catch (thrown: IllegalStateException) {
            thrown
        }

        assertTrue(error.message.orEmpty().contains("terminal state FAILED"))
        assertEquals(listOf(SESSION_ID), playbackApi.stoppedSessions)
        assertTrue(reportedErrors.isEmpty())
    }

    private fun createLiveSessionCoordinator(
        playbackApi: FakePlaybackApi,
        onError: (Throwable) -> Unit = {}
    ): LiveSessionCoordinator {
        return LiveSessionCoordinator(
            playbackApi = playbackApi,
            readinessPoller = ReadinessPoller(playbackApi, PlaybackErrorMapper()),
            heartbeatManager = HeartbeatManager(playbackApi, CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)),
            onSessionUpdated = {},
            onDiagnosticsUpdated = {},
            onError = onError
        )
    }

    private class FakePlaybackApi(
        private val startResult: NativeLiveStartResult,
        private val getSessionState: suspend (String) -> SessionSnapshot
    ) : PlaybackApi {
        val stoppedSessions = mutableListOf<String>()

        override suspend fun ensureAuthSession(authToken: String?) = Unit

        override suspend fun startLiveIntent(request: NativePlaybackRequest.Live): NativeLiveStartResult = startResult

        override suspend fun getSessionState(sessionId: String): SessionSnapshot = getSessionState.invoke(sessionId)

        override suspend fun getRecordingPlaybackInfo(request: NativePlaybackRequest.Recording): io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo? {
            error("recording playback info should not run in these tests")
        }

        override suspend fun getRecordingPlaylistIfReady(recordingId: String): String? {
            error("recording playlist readiness should not run in these tests")
        }

        override suspend fun getPlaybackUrlIfReady(playbackUrl: String): String? {
            error("generic playback url readiness should not run in these tests")
        }

        override suspend fun heartbeat(sessionId: String): SessionSnapshot {
            error("heartbeat should not run in these tests")
        }

        override suspend fun reportPlaybackFeedback(sessionId: String, event: String, code: Int?, message: String?) = Unit

        override suspend fun stopSession(sessionId: String) {
            stoppedSessions += sessionId
        }

        override fun sessionPlaylistUrl(sessionId: String): String = "http://example.invalid/$sessionId.m3u8"

        override fun recordingPlaylistUrl(recordingId: String): String = "http://example.invalid/recording/$recordingId.m3u8"
    }

    private companion object {
        private const val SESSION_ID = "sess-live-1"

        private val TEST_REQUEST = NativePlaybackRequest.Live(
            serviceRef = "1:0:1:AA",
            playbackDecisionToken = "token",
            hwaccel = "auto",
            correlationId = "corr-1",
            title = "Das Erste HD",
            params = mapOf("playback_mode" to "native_hls")
        )
    }
}
