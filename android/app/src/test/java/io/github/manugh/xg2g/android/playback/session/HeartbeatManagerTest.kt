package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.NativeLiveStartResult
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.model.SessionMode
import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import io.github.manugh.xg2g.android.playback.model.SessionState
import io.github.manugh.xg2g.android.playback.net.NativeRecordingPlaybackInfo
import io.github.manugh.xg2g.android.playback.net.PlaybackApi
import io.github.manugh.xg2g.android.transport.playback.PlaybackHttpException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class HeartbeatManagerTest {
    @Test
    fun `transient failure retries and a later success updates the session`() = runTest {
        val api = HeartbeatApi(
            outcomes = ArrayDeque(
                listOf(
                    Result.failure(IllegalStateException("temporary network loss")),
                    Result.success(snapshot())
                )
            )
        )
        val updates = mutableListOf<SessionSnapshot>()
        val errors = mutableListOf<Throwable>()
        val manager = HeartbeatManager(api, this, StandardTestDispatcher(testScheduler))

        manager.start(SESSION_ID, intervalSeconds = 2, updates::add, errors::add)
        advanceTimeBy(2_000)
        runCurrent()
        advanceTimeBy(1_000)
        runCurrent()

        assertEquals(2, api.heartbeatCalls)
        assertEquals(1, updates.size)
        assertEquals(0, errors.size)
        manager.stop()
    }

    @Test
    fun `terminal heartbeat response stops retrying and reports the error`() = runTest {
        val terminal = PlaybackHttpException(410, "gone")
        val api = HeartbeatApi(ArrayDeque(listOf(Result.failure(terminal))))
        val errors = mutableListOf<Throwable>()
        val manager = HeartbeatManager(api, this, StandardTestDispatcher(testScheduler))

        manager.start(SESSION_ID, intervalSeconds = 1, {}, errors::add)
        advanceTimeBy(1_000)
        runCurrent()
        advanceTimeBy(10_000)
        runCurrent()

        assertEquals(1, api.heartbeatCalls)
        assertEquals(listOf(terminal), errors)
    }

    private class HeartbeatApi(
        private val outcomes: ArrayDeque<Result<SessionSnapshot>>
    ) : PlaybackApi {
        var heartbeatCalls = 0

        override suspend fun heartbeat(sessionId: String): SessionSnapshot {
            heartbeatCalls += 1
            return outcomes.removeFirst().getOrThrow()
        }

        override suspend fun ensureAuthSession(authToken: String?) = unsupported()
        override suspend fun startLiveIntent(request: NativePlaybackRequest.Live): NativeLiveStartResult = unsupported()
        override suspend fun getSessionState(sessionId: String): SessionSnapshot = unsupported()
        override suspend fun getRecordingPlaybackInfo(request: NativePlaybackRequest.Recording): NativeRecordingPlaybackInfo? = unsupported()
        override suspend fun getRecordingPlaylistIfReady(recordingId: String): String? = unsupported()
        override suspend fun getPlaybackUrlIfReady(playbackUrl: String): String? = unsupported()
        override suspend fun reportPlaybackFeedback(sessionId: String, event: String, code: Int?, message: String?) = unsupported()
        override suspend fun stopSession(sessionId: String) = unsupported()
        override fun sessionPlaylistUrl(sessionId: String): String = unsupported()
        override fun recordingPlaylistUrl(recordingId: String): String = unsupported()

        private fun unsupported(): Nothing = error("not used")
    }

    private companion object {
        const val SESSION_ID = "3b5f2483-f047-4db7-a16a-a99d4a45ce2a"

        fun snapshot() = SessionSnapshot(
            sessionId = SESSION_ID,
            state = SessionState.Ready,
            playbackUrl = "/api/v3/sessions/$SESSION_ID/hls/index.m3u8",
            mode = SessionMode.Live,
            requestId = null,
            profileReason = null,
            traceJson = null,
            heartbeatIntervalSec = 2,
            leaseExpiresAt = null,
            durationSeconds = null,
            seekableStartSeconds = null,
            seekableEndSeconds = null,
            liveEdgeSeconds = null
        )
    }
}
