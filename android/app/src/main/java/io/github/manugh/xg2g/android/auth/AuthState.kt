package io.github.manugh.xg2g.android.auth

enum class AuthStateKind {
    SignedOut,
    PasskeyRequired,
    PairingRequired,
    DeviceGrantActive,
    Refreshing,
    ReauthRequired,
    Revoked
}

sealed class AuthState {
    abstract val kind: AuthStateKind

    object SignedOut : AuthState() {
        override val kind = AuthStateKind.SignedOut
    }

    data class PasskeyRequired(val setupToken: String? = null) : AuthState() {
        override val kind = AuthStateKind.PasskeyRequired
    }

    data class PairingRequired(val pairingCode: String? = null, val qrUrl: String? = null) : AuthState() {
        override val kind = AuthStateKind.PairingRequired
    }

    data class DeviceGrantActive(
        val deviceGrantId: String,
        val accessToken: String,
        val jktThumbprint: String,
        val expiresAtEpochMs: Long
    ) : AuthState() {
        override val kind = AuthStateKind.DeviceGrantActive
    }

    data class Refreshing(
        val previousGrant: DeviceGrantActive?,
        val attemptCount: Int = 0,
        val lastError: String? = null
    ) : AuthState() {
        override val kind = AuthStateKind.Refreshing
    }

    data class ReauthRequired(val reason: String) : AuthState() {
        override val kind = AuthStateKind.ReauthRequired
    }

    data class Revoked(val reason: String = "Admin revoked device access") : AuthState() {
        override val kind = AuthStateKind.Revoked
    }
}
