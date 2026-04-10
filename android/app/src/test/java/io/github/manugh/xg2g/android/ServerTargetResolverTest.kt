package io.github.manugh.xg2g.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerTargetResolverTest {

    @Test
    fun `normalizeServerUrl adds https scheme and ui base path`() {
        val normalized = ServerTargetResolver.normalizeServerUrl("demo.example")

        assertEquals("https://demo.example/ui/", normalized)
    }

    @Test
    fun `normalizeServerUrl preserves explicit path and port`() {
        val normalized = ServerTargetResolver.normalizeServerUrl("http://demo.example:8080/app")

        assertEquals("http://demo.example:8080/app/", normalized)
    }

    @Test
    fun `resolveConfiguredBaseUrl prefers explicit override`() {
        val resolved = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = "https://saved.example/ui/",
            overrideUrl = "https://override.example/ui/",
            deepLinkUrl = "https://saved.example/ui/live"
        )

        assertEquals("https://override.example/ui/", resolved)
    }

    @Test
    fun `resolveConfiguredBaseUrl reads custom scheme link`() {
        val resolved = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = null,
            overrideUrl = null,
            deepLinkUrl = "xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fui%2F"
        )

        assertEquals("https://tv.example/ui/", resolved)
    }

    @Test
    fun `resolveConfiguredBaseUrl derives ui base from https deep link`() {
        val resolved = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = null,
            overrideUrl = null,
            deepLinkUrl = "https://demo.example/ui/live/channel-1"
        )

        assertEquals("https://demo.example/ui/", resolved)
    }

    @Test
    fun `resolveStartUrl keeps same-origin deep link under base path`() {
        val resolved = ServerTargetResolver.resolveStartUrl(
            baseUrl = "https://demo.example/ui/",
            overrideUrl = null,
            deepLinkUrl = "https://demo.example/ui/live/channel-1"
        )

        assertEquals("https://demo.example/ui/live/channel-1", resolved)
    }

    @Test
    fun `resolveStartUrl ignores deep link outside base path`() {
        val resolved = ServerTargetResolver.resolveStartUrl(
            baseUrl = "https://demo.example/ui/",
            overrideUrl = null,
            deepLinkUrl = "https://demo.example/admin/"
        )

        assertEquals("https://demo.example/ui/", resolved)
    }

    @Test
    fun `same origin comparison respects default ports`() {
        assertTrue(
            ServerTargetResolver.isSameOrigin(
                targetUrl = "https://demo.example/ui/live",
                baseUrl = "https://demo.example:443/ui/"
            )
        )
        assertFalse(
            ServerTargetResolver.isSameOrigin(
                targetUrl = "https://demo.example/ui/live",
                baseUrl = "http://demo.example/ui/"
            )
        )
    }

    @Test
    fun `resolveAuthToken reads custom scheme auth token`() {
        val resolved = ServerTargetResolver.resolveAuthToken(
            overrideToken = null,
            deepLinkUrl = "xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fui%2F&auth_token=token-123"
        )

        assertEquals("token-123", resolved)
    }

    @Test
    fun `resolveAuthToken prefers explicit override`() {
        val resolved = ServerTargetResolver.resolveAuthToken(
            overrideToken = "override-token",
            deepLinkUrl = "xg2g://connect?auth_token=deep-link-token"
        )

        assertEquals("override-token", resolved)
    }

    @Test
    fun `resolveAccessToken reads custom scheme access token`() {
        val resolved = ServerTargetResolver.resolveAccessToken(
            overrideToken = null,
            deepLinkUrl = "xg2g://connect?access_token=device-access-token"
        )

        assertEquals("device-access-token", resolved)
    }

    @Test
    fun `resolveDeviceAuthLaunchCredentials reads device grant and expiry`() {
        val resolved = ServerTargetResolver.resolveDeviceAuthLaunchCredentials(
            overrideDeviceGrantId = null,
            overrideDeviceGrant = null,
            overrideAccessToken = null,
            overrideAccessTokenExpiresAt = null,
            deepLinkUrl = "xg2g://connect?device_grant_id=dgr-123&device_grant=grant-secret&access_token=device-access-token&access_token_expires_at=Thu,%2009%20Apr%202026%2012:00:00%20GMT"
        )

        assertNotNull(resolved)
        assertEquals("dgr-123", resolved?.deviceGrantId)
        assertEquals("grant-secret", resolved?.deviceGrant)
        assertEquals("device-access-token", resolved?.accessToken)
        assertEquals(1775736000000L, resolved?.accessTokenExpiresAtEpochMs)
    }

    @Test
    fun `resolveDeviceAuthLaunchCredentials returns null when no device auth params are present`() {
        val resolved = ServerTargetResolver.resolveDeviceAuthLaunchCredentials(
            overrideDeviceGrantId = null,
            overrideDeviceGrant = null,
            overrideAccessToken = null,
            overrideAccessTokenExpiresAt = null,
            deepLinkUrl = "xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fui%2F"
        )

        assertNull(resolved)
    }
}
