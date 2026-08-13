package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.DeviceAuthTransport
import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import java.io.IOException

internal class NativeDeviceAuthRepository(
    private val stateStore: PersistedDeviceAuthStateStore,
    private val dpopProvider: DPoPProvider,
    val stateMachine: AuthStateMachine,
    private val transport: DeviceAuthTransport,
    private val nowEpochMs: () -> Long = { System.currentTimeMillis() }
) {
    val currentState: AuthState
        get() = stateMachine.currentState

    /**
     * Hydrate state on app restart from Keystore key + persisted DeviceAuthStore.
     */
    suspend fun hydratePersistedStateOnLaunch(baseUrl: String): AuthState {
        val currentStore = stateStore.load()
        val normalizedUrl = baseUrl.trim().trimEnd('/')
        val httpUrl = normalizedUrl.toHttpUrlOrNull()

        if (currentStore == null || currentStore.deviceGrantId.isBlank() || currentStore.deviceGrant.isBlank()) {
            return stateMachine.startEnrollment()
        }

        val jkt = dpopProvider.getJWKThumbprint()
        val expiresAt = currentStore.accessTokenExpiresAtEpochMs ?: 0L
        val now = nowEpochMs()

        // If access token is still valid (more than 30s remaining)
        if (currentStore.accessToken != null && expiresAt > now + 30_000L) {
            return stateMachine.activateDeviceGrant(
                deviceGrantId = currentStore.deviceGrantId,
                accessToken = currentStore.accessToken,
                jktThumbprint = jkt,
                expiresAtEpochMs = expiresAt
            )
        }

        // Access Token expired -> Trigger Refreshing State preserving stored grant context
        stateMachine.activateDeviceGrant(
            deviceGrantId = currentStore.deviceGrantId,
            accessToken = currentStore.accessToken ?: "",
            jktThumbprint = jkt,
            expiresAtEpochMs = expiresAt
        )
        stateMachine.triggerTokenRefresh()

        if (httpUrl == null) {
            return stateMachine.handleRefreshError("Invalid base URL", isNetworkError = true, isRevoked = false)
        }

        return try {
            val refreshed = transport.refreshSession(
                uiBaseUrl = httpUrl,
                deviceGrantId = currentStore.deviceGrantId,
                deviceGrant = currentStore.deviceGrant
            )

            val updatedState = PersistedDeviceAuthState(
                serverUrl = normalizedUrl,
                deviceGrantId = refreshed.rotatedDeviceGrantId ?: currentStore.deviceGrantId,
                deviceGrant = refreshed.rotatedDeviceGrant ?: currentStore.deviceGrant,
                accessSessionId = refreshed.accessSessionId,
                accessToken = refreshed.accessToken,
                accessTokenExpiresAtEpochMs = refreshed.accessTokenExpiresAtEpochMs,
                policyVersion = refreshed.policyVersion,
                publishedEndpoints = refreshed.endpoints
            )
            stateStore.save(updatedState)

            stateMachine.activateDeviceGrant(
                deviceGrantId = updatedState.deviceGrantId,
                accessToken = refreshed.accessToken,
                jktThumbprint = jkt,
                expiresAtEpochMs = refreshed.accessTokenExpiresAtEpochMs
            )
        } catch (e: IOException) {
            // Network Error -> Stay in Refreshing with backoff! NO LOGOUT!
            stateMachine.handleRefreshError(
                errorMsg = e.message ?: "Network error during refresh",
                isNetworkError = true,
                isRevoked = false
            )
        } catch (e: Throwable) {
            val msg = e.message ?: ""
            val isRevoked = msg.contains("revoked", ignoreCase = true) || msg.contains("403", ignoreCase = true) && msg.contains("device", ignoreCase = true)
            val isAuthErr = msg.contains("invalid_grant", ignoreCase = true) || msg.contains("401") || msg.contains("mismatch", ignoreCase = true)

            if (isRevoked) {
                stateMachine.handleRefreshError(msg, isNetworkError = false, isRevoked = true)
            } else if (isAuthErr) {
                stateMachine.handleRefreshError(msg, isNetworkError = false, isRevoked = false)
            } else {
                stateMachine.handleRefreshError(msg, isNetworkError = true, isRevoked = false)
            }
        }
    }
}
