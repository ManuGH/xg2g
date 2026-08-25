package io.github.manugh.xg2g.android.guide

import io.github.manugh.xg2g.android.DeviceAuthRepository
import io.github.manugh.xg2g.android.DeviceAuthTelemetry
import io.github.manugh.xg2g.android.DeviceAuthTelemetryEvent
import io.github.manugh.xg2g.android.DeviceAuthTransport
import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.RefreshedDeviceSession
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.playback.net.AuthCookieSession
import kotlinx.coroutines.runBlocking
import okhttp3.Headers
import okhttp3.HttpUrl
import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.MediaType.Companion.toMediaType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class GuideApiClientTest {
    @Test
    fun `fetchBouquets treats null payload as empty list`() {
        val client = guideApiClient { path ->
            when (path) {
                "/api/v3/services/bouquets" -> "null"
                else -> error("unexpected path $path")
            }
        }

        val bouquets = runSuspend {
            client.fetchBouquets(authToken = null)
        }

        assertTrue(bouquets.isEmpty())
    }

    @Test
    fun `fetchChannels accepts items envelope payload`() {
        val client = guideApiClient { path ->
            when (path) {
                "/api/v3/services" -> """{"items":[{"serviceRef":"1:0:1:abcd","name":"Das Erste HD","number":"1"}]}"""
                else -> error("unexpected path $path")
            }
        }

        val channels = runSuspend {
            client.fetchChannels(authToken = null, bouquetName = null)
        }

        assertEquals(1, channels.size)
        assertEquals("1:0:1:abcd", channels.single().serviceRef)
        assertEquals("Das Erste HD", channels.single().name)
    }

    @Test
    fun `fetchEpgWindow treats null payload as empty schedule`() {
        val client = guideApiClient { path ->
            when (path) {
                "/api/v3/epg" -> "null"
                else -> error("unexpected path $path")
            }
        }

        val schedule = runSuspend {
            client.fetchEpgWindow(
                authToken = null,
                bouquetName = null,
                timelineWindow = GuideTimelineWindow(
                    startEpochSec = 1_700_000_000L,
                    endEpochSec = 1_700_003_600L
                )
            )
        }

        assertTrue(schedule.isEmpty())
    }

    @Test
    fun `fetchEpgWindow preserves xmltv offsets per programme`() {
        val client = guideApiClient { path ->
            when (path) {
                "/api/v3/epg" -> """[{"serviceRef":"1:0:1:abcd","title":"DST Film","start":1774744200,"end":1774747800,"startXmltv":"20260329013000 +0100","endXmltv":"20260329033000 +0200"}]"""
                else -> error("unexpected path $path")
            }
        }

        val schedule = runSuspend {
            client.fetchEpgWindow(
                authToken = null,
                bouquetName = null,
                timelineWindow = GuideTimelineWindow(
                    startEpochSec = 1_700_000_000L,
                    endEpochSec = 1_700_003_600L
                )
            )
        }

        val program = schedule.getValue("1:0:1:ABCD").single()
        assertEquals("20260329013000 +0100", program.startXmltv)
        assertEquals("20260329033000 +0200", program.endXmltv)
    }

    @Test
    fun `fetchHealthStatus parses server time and offset`() {
        val client = guideApiClient { path ->
            when (path) {
                "/api/v3/system/health" -> """{"status":"ok","serverTime":"2026-04-09T22:31:30Z","receiver":{"status":"ok"},"epg":{"status":"ok","missingChannels":0}}"""
                else -> error("unexpected path $path")
            }
        }

        val health = runSuspend {
            client.fetchHealthStatus(authToken = null)
        }

        assertTrue(health.receiverHealthy)
        assertTrue(health.epgHealthy)
        assertEquals(1_775_773_890L, health.serverTimeEpochSec)
        assertEquals(0, health.serverTimeOffsetSeconds)
    }

    @Test
    fun `fetchBouquets succeeds with direct native REST API request`() {
        val cookieSession = MutableCookieSession(hasCookie = true)
        val client = guideApiClient(
            cookieSession = cookieSession
        ) { path ->
            when (path) {
                "/api/v3/services/bouquets" -> "[]"
                else -> error("unexpected path $path")
            }
        }

        val bouquets = runSuspend {
            client.fetchBouquets(authToken = null)
        }

        assertTrue(bouquets.isEmpty())
    }

    private fun guideApiClient(
        cookieSession: AuthCookieSession = AlwaysAuthenticatedCookieSession(),
        responder: (String) -> String
    ): GuideApiClient {
        val okHttpClient = OkHttpClient.Builder()
            .addInterceptor { chain ->
                val request = chain.request()
                val body = responder(request.url.encodedPath)
                Response.Builder()
                    .request(request)
                    .protocol(Protocol.HTTP_1_1)
                    .code(200)
                    .message("OK")
                    .body(body.toResponseBody("application/json".toMediaType()))
                    .build()
            }
            .build()

        val mockStore = object : io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore {
            override fun load() = null
            override fun save(state: io.github.manugh.xg2g.android.PersistedDeviceAuthState) {}
            override fun clear() {}
        }
        val mockDPoP = object : DPoPProvider {
            override fun createProof(htm: String, htu: String, ath: String?) = "proof"
            override fun getOrGenerateKeyPair(): java.security.KeyPair = java.security.KeyPairGenerator.getInstance("EC").apply { initialize(256) }.generateKeyPair()
            override fun getJWKThumbprint() = "jkt"
        }

        return GuideApiClient(
            baseUrl = "http://127.0.0.1:8080/ui/",
            stateStore = mockStore,
            dpopProvider = mockDPoP,
            okHttpClient = okHttpClient
        )
    }

    private fun <T> runSuspend(block: suspend () -> T): T = runBlocking { block() }
}

private class AlwaysAuthenticatedCookieSession : AuthCookieSession {
    override fun hasSessionCookie(url: HttpUrl, cookieName: String): Boolean = true

    override fun applyCookies(url: HttpUrl, builder: Request.Builder) = Unit

    override fun storeCookies(url: HttpUrl, headers: Headers) = Unit

    override fun cookieHeader(url: HttpUrl): String? = "xg2g_session=test"

    override fun clearSessionCookie(url: HttpUrl, cookieName: String, cookiePath: String) = Unit
}

private class MutableCookieSession(
    private var hasCookie: Boolean
) : AuthCookieSession {
    override fun hasSessionCookie(url: HttpUrl, cookieName: String): Boolean = hasCookie

    override fun applyCookies(url: HttpUrl, builder: Request.Builder) = Unit

    override fun storeCookies(url: HttpUrl, headers: Headers) {
        hasCookie = true
    }

    override fun cookieHeader(url: HttpUrl): String? = if (hasCookie) "xg2g_session=test" else null

    override fun clearSessionCookie(url: HttpUrl, cookieName: String, cookiePath: String) {
        hasCookie = false
    }
}

private class TestStateStore(
    var current: PersistedDeviceAuthState? = null
) : PersistedDeviceAuthStateStore {
    override fun load(): PersistedDeviceAuthState? = current

    override fun save(state: PersistedDeviceAuthState) {
        current = state
    }

    override fun clear() {
        current = null
    }
}

private class RecordingDeviceAuthTransport : DeviceAuthTransport {
    var refreshResponse: RefreshedDeviceSession? = null
    var refreshCalls = 0
    var createCookieSessionCalls = 0

    override suspend fun refreshSession(
        uiBaseUrl: HttpUrl,
        deviceGrantId: String,
        deviceGrant: String
    ): RefreshedDeviceSession {
        refreshCalls += 1
        return refreshResponse ?: error("refreshResponse not configured")
    }

    override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {
        createCookieSessionCalls += 1
    }
}
