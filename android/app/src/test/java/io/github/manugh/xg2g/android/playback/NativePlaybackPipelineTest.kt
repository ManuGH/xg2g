package io.github.manugh.xg2g.android.playback

import io.github.manugh.xg2g.android.auth.SoftwareDPoPProvider
import io.github.manugh.xg2g.android.playback.player.AudioOutputMode
import io.github.manugh.xg2g.android.playback.player.AudioPassthroughConfig
import io.github.manugh.xg2g.android.playback.player.Media3SessionBinder
import io.github.manugh.xg2g.android.playback.player.PlaybackSessionBinding
import io.github.manugh.xg2g.android.playback.session.PlaybackPreemptionHandler
import io.github.manugh.xg2g.android.playback.session.PreemptionOutcome
import okhttp3.Interceptor
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NativePlaybackPipelineTest {

    @Test
    fun `Media3SessionBinder injects dynamic DPoP proof and Authorization DPoP header`() {
        var recordedAuth: String? = null
        var recordedDpop: String? = null
        var recordedProfile: String? = null
        var recordedDecisionToken: String? = null

        val dpopProvider = SoftwareDPoPProvider()
        val binder = Media3SessionBinder(dpopProvider)
        val binding = PlaybackSessionBinding(
            sessionId = "sess_100",
            playbackDecisionToken = "token_decision_abc",
            accessToken = "at_dpop_xyz",
            profileId = "prof_child"
        )
        val boundClientWithoutMock = binder.createBoundOkHttpClient(OkHttpClient(), binding)

        val mockInterceptor = Interceptor { chain ->
            val req = chain.request()
            recordedAuth = req.header("Authorization")
            recordedDpop = req.header("DPoP")
            recordedProfile = req.header("X-Household-Profile")
            recordedDecisionToken = req.header("X-XG2G-Playback-Decision-Token")

            Response.Builder()
                .request(req)
                .protocol(Protocol.HTTP_1_1)
                .code(200)
                .message("OK")
                .body("OK".toResponseBody("text/plain".toMediaType()))
                .build()
        }

        val testClient = boundClientWithoutMock.newBuilder().addInterceptor(mockInterceptor).build()

        val request = Request.Builder().url("https://xg2g.local/api/v3/sessions/sess_100/hls/index.m3u8").build()
        testClient.newCall(request).execute()

        assertEquals("DPoP at_dpop_xyz", recordedAuth)
        assertNotNull(recordedDpop)
        assertTrue(recordedDpop!!.startsWith("eyJ"))
        assertEquals("prof_child", recordedProfile)
        assertEquals("token_decision_abc", recordedDecisionToken)
    }

    @Test
    fun `PlaybackPreemptionHandler correctly classifies HTTP 409, 401, and Push events`() {
        val handler = PlaybackPreemptionHandler()

        // 1. HTTP 409 Conflict -> Preempted
        val res409 = handler.evaluateHttpStatus(409, "Priorisierter Timer gestartet")
        assertTrue(res409 is PreemptionOutcome.Preempted)
        assertEquals("Priorisierter Timer gestartet", (res409 as PreemptionOutcome.Preempted).reason)

        // 2. HTTP 401 Unauthorized -> Revoked
        val res401 = handler.evaluateHttpStatus(401)
        assertTrue(res401 is PreemptionOutcome.Revoked)

        // 3. Push stream_preempted -> Preempted
        val pushPreempt = handler.evaluatePushEvent("stream_preempted", "Recording preempted live stream")
        assertTrue(pushPreempt is PreemptionOutcome.Preempted)
        assertEquals("Recording preempted live stream", (pushPreempt as PreemptionOutcome.Preempted).reason)

        // 4. Push device_revoked -> Revoked
        val pushRevoke = handler.evaluatePushEvent("device_revoked")
        assertTrue(pushRevoke is PreemptionOutcome.Revoked)
    }

    @Test
    fun `AudioPassthroughConfig resolves passthrough and stereo modes`() {
        val config = AudioPassthroughConfig()

        assertEquals(AudioOutputMode.PASSTHROUGH, config.resolveAudioMode("passthrough"))
        assertEquals(AudioOutputMode.PASSTHROUGH, config.resolveAudioMode("ac3"))
        assertEquals(AudioOutputMode.STEREO, config.resolveAudioMode("stereo"))
        assertEquals(AudioOutputMode.STEREO, config.resolveAudioMode(null))
    }
}
