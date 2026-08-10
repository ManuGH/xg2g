package io.github.manugh.xg2g.android.settings

import android.content.Context
import androidx.compose.runtime.Immutable
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import io.github.manugh.xg2g.android.ServerSettingsStore
import io.github.manugh.xg2g.android.dashboard.DashboardApiClient
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

@Immutable
internal data class SettingsUiState(
    val serverUrl: String = "",
    val authToken: String? = null,
    val receiverStatus: String = "Lade...",
    val epgStatus: String = "Lade...",
    val appVersion: String = "2.0.0 (Native Compose TV)",
    val isSaving: Boolean = false,
    val message: String? = null
)

internal class SettingsViewModel(
    private val serverSettingsStore: ServerSettingsStore,
    private val dashboardApiClient: DashboardApiClient
) : ViewModel() {

    private val _uiState = MutableStateFlow(
        SettingsUiState(
            serverUrl = serverSettingsStore.getServerUrl().orEmpty(),
            authToken = serverSettingsStore.getAuthToken()
        )
    )
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    init {
        refreshHealth()
    }

    fun refreshHealth() {
        viewModelScope.launch {
            val url = serverSettingsStore.getServerUrl().orEmpty()
            val token = serverSettingsStore.getAuthToken()
            _uiState.value = _uiState.value.copy(
                serverUrl = url,
                authToken = token
            )

            try {
                val health = dashboardApiClient.fetchHealth(token)
                _uiState.value = _uiState.value.copy(
                    receiverStatus = if (health.receiverHealthy) "ONLINE (Bereit)" else "OFFLINE",
                    epgStatus = if (health.epgHealthy) "AKTIV (Synchronisiert)" else "EINGESCHRÄNKT"
                )
            } catch (e: Exception) {
                _uiState.value = _uiState.value.copy(
                    receiverStatus = "Nicht erreichbar",
                    epgStatus = "Nicht verfügbar"
                )
            }
        }
    }

    fun saveToken(token: String?) {
        viewModelScope.launch {
            _uiState.value = _uiState.value.copy(isSaving = true)
            serverSettingsStore.saveAuthToken(token)
            _uiState.value = _uiState.value.copy(
                authToken = token?.trim()?.takeIf { it.isNotEmpty() },
                isSaving = false,
                message = "API-Token erfolgreich gespeichert."
            )
            refreshHealth()
        }
    }

    fun clearMessage() {
        _uiState.value = _uiState.value.copy(message = null)
    }

    class Factory(
        private val context: Context,
        private val serverUrl: String
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            val store = ServerSettingsStore(context)
            val client = DashboardApiClient(serverUrl)
            return SettingsViewModel(
                serverSettingsStore = store,
                dashboardApiClient = client
            ) as T
        }
    }
}
