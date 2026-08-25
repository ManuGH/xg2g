package io.github.manugh.xg2g.android.transport.auth

import io.github.manugh.xg2g.android.DeviceAuthLaunchCredentials
import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.transport.DeviceAuthTransport
import java.io.IOException
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

internal class NativeDeviceAuthRepository(
    val stateStore: PersistedDeviceAuthStateStore,
    val dpopProvider: DPoPProvider,
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

    fun applyLaunchCredentials(baseUrl: String, credentials: DeviceAuthLaunchCredentials?) {
        val normalizedBaseUrl = baseUrl.trim().trimEnd('/')
        if (credentials == null || normalizedBaseUrl.isBlank()) return

        val currentStore = stateStore.load()
        val jkt = dpopProvider.getJWKThumbprint()

        when {
            credentials.hasPersistableGrant() -> {
                val grantId = credentials.deviceGrantId!!.trim()
                val grant = credentials.deviceGrant!!.trim()
                val token = credentials.accessToken?.trim()?.takeIf { it.isNotEmpty() }
                val expiresAt = credentials.accessTokenExpiresAtEpochMs ?: 0L

                val updatedState = PersistedDeviceAuthState(
                    serverUrl = normalizedBaseUrl,
                    deviceGrantId = grantId,
                    deviceGrant = grant,
                    accessSessionId = currentStore?.accessSessionId,
                    accessToken = token,
                    accessTokenExpiresAtEpochMs = expiresAt,
                    policyVersion = currentStore?.policyVersion,
                    publishedEndpoints = currentStore?.publishedEndpoints.orEmpty()
                )
                stateStore.save(updatedState)
                if (token != null) {
                    stateMachine.activateDeviceGrant(grantId, token, jkt, expiresAt)
                }
            }

            currentStore != null && !credentials.accessToken.isNullOrBlank() -> {
                val token = credentials.accessToken.trim()
                val expiresAt = credentials.accessTokenExpiresAtEpochMs ?: 0L
                val updatedState = currentStore.copy(
                    accessToken = token,
                    accessTokenExpiresAtEpochMs = expiresAt
                )
                stateStore.save(updatedState)
                stateMachine.activateDeviceGrant(currentStore.deviceGrantId, token, jkt, expiresAt)
            }
        }
    }

    fun clearPersistedState() {
        stateStore.clear()
        stateMachine.handleRefreshError("Cleared state", isNetworkError = false, isRevoked = true)
    }

    fun prepareWebSession(baseUrl: String, targetUrl: String, legacyAuthToken: String?): String {
        return targetUrl
    }
}
