package io.github.manugh.xg2g.android

import io.github.manugh.xg2g.android.playback.net.AuthCookieSession
import okhttp3.Headers
import okhttp3.HttpUrl
import okhttp3.Request
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class DeviceAuthRepositoryTest {

    @Test
    fun `ensureAuthSession refreshes rotated grant before creating cookie session`() {
        val store = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "https://demo.example/ui/",
                deviceGrantId = "dgr-old",
                deviceGrant = "secret-old",
                accessToken = "stale-token",
                accessTokenExpiresAtEpochMs = 10L
            )
        )
        val transport = FakeTransport().apply {
            refreshResponse = RefreshedDeviceSession(
                rotatedDeviceGrantId = "dgr-new",
                rotatedDeviceGrant = "secret-new",
                accessSessionId = "dss-1",
                accessToken = "fresh-token",
                accessTokenExpiresAtEpochMs = 120_000L,
                policyVersion = "device-auth-v1",
                endpoints = listOf(
                    PublishedEndpoint(
                        url = "https://public.example",
                        kind = "public_https",
                        priority = 10,
                        tlsMode = "required",
                        allowPairing = true,
                        allowStreaming = true,
                        allowWeb = true,
                        allowNative = true,
                        advertiseReason = "public reverse proxy",
                        source = "config"
                    )
                )
            )
        }
        val cookies = FakeCookieSession()
        val repository = DeviceAuthRepository(
            stateStore = store,
            cookieSession = cookies,
            transport = transport,
            telemetry = FakeTelemetry(),
            nowEpochMs = { 60_000L }
        )

        runSuspend {
            repository.ensureAuthSession("https://demo.example/ui/", legacyAuthToken = null)
        }

        assertEquals("fresh-token", transport.createdCookieSessionBearer)
        assertEquals("dgr-new", store.current?.deviceGrantId)
        assertEquals("secret-new", store.current?.deviceGrant)
        assertEquals("fresh-token", store.current?.accessToken)
        assertEquals("https://public.example/ui/", store.current?.serverUrl)
    }

    @Test
    fun `ensureAuthSession ignores stale cookie shortcut when persisted device auth exists`() {
        val store = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "https://demo.example/ui/",
                deviceGrantId = "dgr-1",
                deviceGrant = "grant-secret"
            )
        )
        val cookies = FakeCookieSession().apply {
            hasCookie = true
        }
        val transport = FakeTransport().apply {
            refreshResponse = RefreshedDeviceSession(
                accessSessionId = "dss-1",
                accessToken = "fresh-token",
                accessTokenExpiresAtEpochMs = 120_000L,
                policyVersion = "device-auth-v1"
            )
        }
        val repository = DeviceAuthRepository(
            stateStore = store,
            cookieSession = cookies,
            transport = transport,
            telemetry = FakeTelemetry(),
            nowEpochMs = { 60_000L }
        )

        runSuspend {
            repository.ensureAuthSession("https://demo.example/ui/", legacyAuthToken = null)
        }

        assertEquals(1, transport.refreshCalls)
        assertEquals(1, transport.createCookieSessionCalls)
        assertEquals("fresh-token", transport.createdCookieSessionBearer)
        assertEquals("fresh-token", store.current?.accessToken)

        runSuspend {
            repository.ensureAuthSession("https://demo.example/ui/", legacyAuthToken = null)
        }

        assertEquals(1, transport.refreshCalls)
        assertEquals(1, transport.createCookieSessionCalls)
    }

    @Test
    fun `ensureAuthSession falls back to legacy auth token without device grant`() {
        val transport = FakeTransport()
        val repository = DeviceAuthRepository(
            stateStore = FakeStateStore(),
            cookieSession = FakeCookieSession(),
            transport = transport,
            telemetry = FakeTelemetry(),
            nowEpochMs = { 0L }
        )

        runSuspend {
            repository.ensureAuthSession("https://demo.example/ui/", legacyAuthToken = "legacy-token")
        }

        assertEquals("legacy-token", transport.createdCookieSessionBearer)
        assertNull(transport.refreshedGrantId)
    }

    @Test
    fun `prepareWebSession uses web bootstrap for persisted device auth`() {
        val cookies = FakeCookieSession()
        val transport = FakeTransport().apply {
            startBootstrapResponse = StartedWebBootstrap(
                completePath = "/api/v3/auth/web-bootstrap/wbs-1",
                bootstrapToken = "bootstrap-token"
            )
            completeBootstrapResponse = CompletedWebBootstrap(
                locationPath = "/ui/dashboard?mode=tv"
            )
        }
        val repository = DeviceAuthRepository(
            stateStore = FakeStateStore(
                PersistedDeviceAuthState(
                    serverUrl = "https://demo.example/ui/",
                    deviceGrantId = "dgr-1",
                    deviceGrant = "grant-secret",
                    accessToken = "device-access",
                    accessTokenExpiresAtEpochMs = 120_000L
                )
            ),
            cookieSession = cookies,
            transport = transport,
            telemetry = FakeTelemetry(),
            nowEpochMs = { 60_000L }
        )

        val preparedUrl = runSuspend {
            repository.prepareWebSession(
                baseUrl = "https://demo.example/ui/",
                targetUrl = "https://demo.example/ui/dashboard?mode=tv",
                legacyAuthToken = null
            )
        }

        assertEquals("/ui/dashboard?mode=tv", transport.startedBootstrapTargetPath)
        assertEquals(
            "https://demo.example/ui/dashboard?mode=tv",
            preparedUrl
        )
        assertTrue(transport.completedBootstrap)
        assertNull(transport.createdCookieSessionBearer)
    }

    @Test
    fun `prepareWebSession ignores stale cookie shortcut when persisted device auth exists`() {
        val cookies = FakeCookieSession().apply {
            hasCookie = true
        }
        val transport = FakeTransport().apply {
            startBootstrapResponse = StartedWebBootstrap(
                completePath = "/api/v3/auth/web-bootstrap/wbs-2",
                bootstrapToken = "bootstrap-token"
            )
            completeBootstrapResponse = CompletedWebBootstrap(
                locationPath = "/ui/"
            )
        }
        val repository = DeviceAuthRepository(
            stateStore = FakeStateStore(
                PersistedDeviceAuthState(
                    serverUrl = "https://demo.example/ui/",
                    deviceGrantId = "dgr-1",
                    deviceGrant = "grant-secret",
                    accessToken = "device-access",
                    accessTokenExpiresAtEpochMs = 120_000L
                )
            ),
            cookieSession = cookies,
            transport = transport,
            telemetry = FakeTelemetry(),
            nowEpochMs = { 60_000L }
        )

        val preparedUrl = runSuspend {
            repository.prepareWebSession(
                baseUrl = "https://demo.example/ui/",
                targetUrl = "https://demo.example/ui/",
                legacyAuthToken = null
            )
        }

        assertEquals("/ui/", transport.startedBootstrapTargetPath)
        assertTrue(transport.completedBootstrap)
        assertEquals("https://demo.example/ui/", preparedUrl)
    }

    @Test
    fun `ensureAuthSession accepts a launch base URL that matches a published endpoint`() {
        val store = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "https://demo.example/ui/",
                deviceGrantId = "dgr-1",
                deviceGrant = "grant-secret",
                publishedEndpoints = listOf(
                    PublishedEndpoint(
                        url = "https://edge.example",
                        kind = "public_https",
                        priority = 10,
                        tlsMode = "required",
                        allowPairing = true,
                        allowStreaming = true,
                        allowWeb = true,
                        allowNative = true,
                        advertiseReason = "public edge",
                        source = "config"
                    )
                )
            )
        )
        val transport = FakeTransport().apply {
            refreshResponse = RefreshedDeviceSession(
                accessSessionId = "dss-1",
                accessToken = "fresh-token",
                accessTokenExpiresAtEpochMs = 120_000L,
                policyVersion = "device-auth-v1"
            )
        }
        val repository = DeviceAuthRepository(
            stateStore = store,
            cookieSession = FakeCookieSession(),
            transport = transport,
            telemetry = FakeTelemetry(),
            nowEpochMs = { 60_000L }
        )

        runSuspend {
            repository.ensureAuthSession("https://edge.example/ui/", legacyAuthToken = null)
        }

        assertEquals(1, transport.refreshCalls)
        assertEquals("https://edge.example/ui/", store.current?.serverUrl)
    }

    @Test
    fun `applyLaunchCredentials persists grant and access token`() {
        val store = FakeStateStore()
        val repository = DeviceAuthRepository(
            stateStore = store,
            cookieSession = FakeCookieSession(),
            transport = FakeTransport(),
            telemetry = FakeTelemetry()
        )

        repository.applyLaunchCredentials(
            baseUrl = "https://demo.example/ui/",
            credentials = DeviceAuthLaunchCredentials(
                deviceGrantId = " dgr-1 ",
                deviceGrant = " grant-secret ",
                accessToken = " access-token ",
                accessTokenExpiresAtEpochMs = 123_456L
            )
        )

        assertEquals("https://demo.example/ui/", store.current?.serverUrl)
        assertEquals("dgr-1", store.current?.deviceGrantId)
        assertEquals("grant-secret", store.current?.deviceGrant)
        assertEquals("access-token", store.current?.accessToken)
        assertEquals(123_456L, store.current?.accessTokenExpiresAtEpochMs)
    }

    @Test
    fun `ensureAuthSession clears device grant and cookie when refresh is revoked`() {
        val store = FakeStateStore(
            PersistedDeviceAuthState(
                serverUrl = "https://demo.example/ui/",
                deviceGrantId = "dgr-1",
                deviceGrant = "grant-secret"
            )
        )
        val cookies = FakeCookieSession()
        val telemetry = FakeTelemetry()
        val transport = FakeTransport().apply {
            refreshException = DeviceAuthHttpException(
                statusCode = 410,
                problemType = "auth/device_session/revoked",
                message = "grant revoked"
            )
        }
        val repository = DeviceAuthRepository(
            stateStore = store,
            cookieSession = cookies,
            transport = transport,
            telemetry = telemetry,
            nowEpochMs = { 60_000L }
        )

        try {
            runSuspend {
                repository.ensureAuthSession("https://demo.example/ui/", legacyAuthToken = null)
            }
            fail("expected DeviceAuthReenrollRequiredException")
        } catch (expected: DeviceAuthReenrollRequiredException) {
            assertEquals("Android device pairing is no longer valid. Pair this device again.", expected.message)
        }

        assertNull(store.current)
        assertEquals(1, cookies.clearCalls)
        assertEquals("device_auth_reenroll_required", telemetry.events.lastOrNull()?.name)
    }

    @Test
    fun `ensureAuthSession records legacy auth token fallback telemetry`() {
        val telemetry = FakeTelemetry()
        val repository = DeviceAuthRepository(
            stateStore = FakeStateStore(),
            cookieSession = FakeCookieSession(),
            transport = FakeTransport(),
            telemetry = telemetry,
            nowEpochMs = { 0L }
        )

        runSuspend {
            repository.ensureAuthSession("https://demo.example/ui/", legacyAuthToken = "legacy-token")
        }

        assertEquals("legacy_auth_token_fallback", telemetry.events.single().name)
        assertEquals("ensure_auth_session", telemetry.events.single().stage)
    }

    private fun <T> runSuspend(block: suspend () -> T): T {
        return kotlinx.coroutines.runBlocking { block() }
    }
}

private class FakeStateStore(
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

private class FakeCookieSession : AuthCookieSession {
    var hasCookie = false
    var clearCalls = 0

    override fun hasSessionCookie(url: HttpUrl, cookieName: String): Boolean = hasCookie

    override fun applyCookies(url: HttpUrl, builder: Request.Builder) = Unit

    override fun storeCookies(url: HttpUrl, headers: Headers) {
        hasCookie = true
    }

    override fun cookieHeader(url: HttpUrl): String? = null

    override fun clearSessionCookie(url: HttpUrl, cookieName: String, cookiePath: String) {
        hasCookie = false
        clearCalls += 1
    }
}

private class FakeTransport : DeviceAuthTransport {
    var refreshResponse: RefreshedDeviceSession? = null
    var refreshException: DeviceAuthHttpException? = null
    var startBootstrapResponse: StartedWebBootstrap? = null
    var completeBootstrapResponse: CompletedWebBootstrap? = null
    var refreshCalls = 0
    var createCookieSessionCalls = 0
    var refreshedGrantId: String? = null
    var refreshedGrantSecret: String? = null
    var createdCookieSessionBearer: String? = null
    var startedBootstrapTargetPath: String? = null
    var completedBootstrap = false

    override suspend fun refreshSession(
        uiBaseUrl: HttpUrl,
        deviceGrantId: String,
        deviceGrant: String
    ): RefreshedDeviceSession {
        refreshCalls += 1
        refreshException?.let { throw it }
        refreshedGrantId = deviceGrantId
        refreshedGrantSecret = deviceGrant
        return refreshResponse
            ?: throw AssertionError("refreshSession called without configured response")
    }

    override suspend fun createCookieSession(uiBaseUrl: HttpUrl, bearerToken: String) {
        createCookieSessionCalls += 1
        createdCookieSessionBearer = bearerToken
    }

    override suspend fun startWebBootstrap(
        uiBaseUrl: HttpUrl,
        accessToken: String,
        targetPath: String
    ): StartedWebBootstrap {
        startedBootstrapTargetPath = targetPath
        return startBootstrapResponse
            ?: throw AssertionError("startWebBootstrap called without configured response")
    }

    override suspend fun completeWebBootstrap(
        uiBaseUrl: HttpUrl,
        completePath: String,
        bootstrapToken: String
    ): CompletedWebBootstrap {
        completedBootstrap = true
        return completeBootstrapResponse
            ?: throw AssertionError("completeWebBootstrap called without configured response")
    }
}

private class FakeTelemetry : DeviceAuthTelemetry {
    val events = mutableListOf<DeviceAuthTelemetryEvent>()

    override fun record(event: DeviceAuthTelemetryEvent) {
        events += event
    }
}
