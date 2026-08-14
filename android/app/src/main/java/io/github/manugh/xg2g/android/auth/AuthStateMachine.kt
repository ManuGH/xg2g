package io.github.manugh.xg2g.android.auth

import java.util.concurrent.atomic.AtomicReference

class AuthStateMachine(
    initialState: AuthState = AuthState.SignedOut,
    val isTvDevice: Boolean = false
) {
    private val stateRef = AtomicReference<AuthState>(initialState)

    val currentState: AuthState
        get() = stateRef.get()

    /**
     * Start enrollment based on form factor:
     * Phone -> PasskeyRequired
     * TV -> PairingRequired
     */
    fun startEnrollment(setupToken: String? = null, pairingCode: String? = null, qrUrl: String? = null): AuthState {
        val nextState = if (isTvDevice) {
            AuthState.PairingRequired(pairingCode, qrUrl)
        } else {
            AuthState.PasskeyRequired(setupToken)
        }
        stateRef.set(nextState)
        return nextState
    }

    /**
     * Complete enrollment or activate device grant.
     */
    fun activateDeviceGrant(
        deviceGrantId: String,
        accessToken: String,
        jktThumbprint: String,
        expiresAtEpochMs: Long
    ): AuthState {
        val current = currentState
        if (current is AuthState.Revoked) {
            throw IllegalStateException("Cannot activate device grant directly from Revoked state without explicit re-enrollment")
        }
        val nextState = AuthState.DeviceGrantActive(
            deviceGrantId = deviceGrantId,
            accessToken = accessToken,
            jktThumbprint = jktThumbprint,
            expiresAtEpochMs = expiresAtEpochMs
        )
        stateRef.set(nextState)
        return nextState
    }

    /**
     * Transition to Refreshing when access token expires.
     */
    fun triggerTokenRefresh(): AuthState {
        val current = currentState
        if (current is AuthState.Revoked) {
            throw IllegalStateException("Cannot refresh token from Revoked state")
        }
        val prevActive = current as? AuthState.DeviceGrantActive
        val nextState = AuthState.Refreshing(previousGrant = prevActive, attemptCount = 1)
        stateRef.set(nextState)
        return nextState
    }

    /**
     * Handle refresh result:
     * - Network Error -> Stay in Refreshing with incremented attempt count and backoff
     * - Auth Error (invalid_grant / key mismatch) -> ReauthRequired
     * - Revoked Error -> Revoked
     */
    fun handleRefreshError(errorMsg: String, isNetworkError: Boolean, isRevoked: Boolean): AuthState {
        val current = currentState
        if (current is AuthState.Revoked) {
            return current
        }

        if (isRevoked) {
            val nextState = AuthState.Revoked(reason = errorMsg)
            stateRef.set(nextState)
            return nextState
        }

        if (isNetworkError) {
            val prevRefreshing = current as? AuthState.Refreshing
            val prevActive = prevRefreshing?.previousGrant ?: (current as? AuthState.DeviceGrantActive)
            val nextAttempt = (prevRefreshing?.attemptCount ?: 0) + 1
            val nextState = AuthState.Refreshing(
                previousGrant = prevActive,
                attemptCount = nextAttempt,
                lastError = errorMsg
            )
            stateRef.set(nextState)
            return nextState
        }

        // True Auth Error (invalid_grant, key mismatch, expired grant)
        val nextState = AuthState.ReauthRequired(reason = errorMsg)
        stateRef.set(nextState)
        return nextState
    }

    /**
     * Revoke device access (Admin/Server initiated).
     */
    fun revoke(reason: String = "Admin revoked access"): AuthState {
        val nextState = AuthState.Revoked(reason = reason)
        stateRef.set(nextState)
        return nextState
    }

    /**
     * Reset from Revoked or ReauthRequired to fresh enrollment via explicit user action.
     */
    fun resetToEnrollment(setupToken: String? = null, pairingCode: String? = null, qrUrl: String? = null): AuthState {
        return startEnrollment(setupToken, pairingCode, qrUrl)
    }

    /**
     * Clear all state and sign out.
     */
    fun signOut(): AuthState {
        val nextState = AuthState.SignedOut
        stateRef.set(nextState)
        return nextState
    }
}
