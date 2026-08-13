package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.MainUiState

internal object AuthUiMapper {
    fun mapToUiState(authState: AuthState, isTvDevice: Boolean): MainUiState {
        return when (authState) {
            is AuthState.SignedOut -> {
                MainUiState.Setup(savedUrl = null)
            }
            is AuthState.PasskeyRequired -> {
                MainUiState.Setup(savedUrl = null)
            }
            is AuthState.PairingRequired -> {
                if (isTvDevice) {
                    MainUiState.TvHome(serverLabel = authState.pairingCode ?: "ABCD-EFGH")
                } else {
                    MainUiState.Setup(savedUrl = null)
                }
            }
            is AuthState.DeviceGrantActive -> {
                MainUiState.Content
            }
            is AuthState.Refreshing -> {
                MainUiState.RefreshingBanner(
                    attemptCount = authState.attemptCount,
                    lastError = authState.lastError
                )
            }
            is AuthState.ReauthRequired -> {
                MainUiState.ReauthRequired(reason = authState.reason)
            }
            is AuthState.Revoked -> {
                MainUiState.Revoked(reason = authState.reason)
            }
        }
    }
}
