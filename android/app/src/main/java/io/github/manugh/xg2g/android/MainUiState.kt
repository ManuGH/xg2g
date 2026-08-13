package io.github.manugh.xg2g.android

import io.github.manugh.xg2g.android.auth.AuthState

internal sealed interface MainUiState {
    data class TvHome(val serverLabel: String) : MainUiState
    data class Setup(val savedUrl: String?) : MainUiState
    data class Error(val title: String, val detail: String) : MainUiState
    data class Loading(val destinationLabel: String? = null) : MainUiState
    data class Revoked(val reason: String) : MainUiState
    data class ReauthRequired(val reason: String) : MainUiState
    data class RefreshingBanner(val attemptCount: Int, val lastError: String?) : MainUiState
    object Content : MainUiState
}
