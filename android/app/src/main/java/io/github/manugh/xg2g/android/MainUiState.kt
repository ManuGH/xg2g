package io.github.manugh.xg2g.android

internal sealed interface MainUiState {
    data class Setup(val savedUrl: String?) : MainUiState
    data class Error(val title: String, val detail: String) : MainUiState
    data class Loading(val destinationLabel: String? = null) : MainUiState
    object Content : MainUiState
}
