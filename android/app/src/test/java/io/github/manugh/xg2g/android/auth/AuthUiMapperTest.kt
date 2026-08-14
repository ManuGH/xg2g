package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.MainUiState
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AuthUiMapperTest {

    @Test
    fun `mapToUiState maps Revoked state cleanly`() {
        val authState = AuthState.Revoked(reason = "Admin deleted device grant")
        val uiState = AuthUiMapper.mapToUiState(authState, isTvDevice = false)

        assertTrue(uiState is MainUiState.Revoked)
        assertEquals("Admin deleted device grant", (uiState as MainUiState.Revoked).reason)
    }

    @Test
    fun `mapToUiState maps ReauthRequired state cleanly`() {
        val authState = AuthState.ReauthRequired(reason = "HTTP 401 invalid_grant")
        val uiState = AuthUiMapper.mapToUiState(authState, isTvDevice = false)

        assertTrue(uiState is MainUiState.ReauthRequired)
        assertEquals("HTTP 401 invalid_grant", (uiState as MainUiState.ReauthRequired).reason)
    }

    @Test
    fun `mapToUiState maps Refreshing state to non-blocking RefreshingBanner`() {
        val prevActive = AuthState.DeviceGrantActive("dgr_1", "at_1", "jkt_1", 100L)
        val authState = AuthState.Refreshing(previousGrant = prevActive, attemptCount = 3, lastError = "Timeout")
        val uiState = AuthUiMapper.mapToUiState(authState, isTvDevice = false)

        assertTrue(uiState is MainUiState.RefreshingBanner)
        val banner = uiState as MainUiState.RefreshingBanner
        assertEquals(3, banner.attemptCount)
        assertEquals("Timeout", banner.lastError)
    }

    @Test
    fun `mapToUiState maps DeviceGrantActive to Content`() {
        val authState = AuthState.DeviceGrantActive("dgr_1", "at_1", "jkt_1", 100L)
        val uiState = AuthUiMapper.mapToUiState(authState, isTvDevice = false)

        assertEquals(MainUiState.Content, uiState)
    }

    @Test
    fun `mapToUiState maps TV PairingRequired to TvHome with code`() {
        val authState = AuthState.PairingRequired(pairingCode = "ABCD-EFGH", qrUrl = "https://xg2g/pair")
        val uiState = AuthUiMapper.mapToUiState(authState, isTvDevice = true)

        assertTrue(uiState is MainUiState.TvHome)
        assertEquals("ABCD-EFGH", (uiState as MainUiState.TvHome).serverLabel)
    }
}
