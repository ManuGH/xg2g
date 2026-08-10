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
    val message: String? = null,
    val isPairingActive: Boolean = false,
    val pairingCode: String? = null,
    val pairingQrPayload: String? = null,
    val pairingError: String? = null,
    val pairingStatus: String = "idle"
)

internal class SettingsViewModel(
    private val serverSettingsStore: ServerSettingsStore,
    private val dashboardApiClient: DashboardApiClient,
    private val pairingApiClient: io.github.manugh.xg2g.android.pairing.PairingApiClient? = null
) : ViewModel() {

    private val _uiState = MutableStateFlow(
        SettingsUiState(
            serverUrl = serverSettingsStore.getServerUrl().orEmpty(),
            authToken = serverSettingsStore.getAuthToken()
        )
    )
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    private var activePairingSecret: String? = null
    private var activePairingId: String? = null

    init {
        refreshHealth()
    }

    fun startPairing() {
        val client = pairingApiClient ?: io.github.manugh.xg2g.android.pairing.PairingApiClient(serverSettingsStore.getServerUrl().orEmpty())
        viewModelScope.launch {
            _uiState.value = _uiState.value.copy(
                isPairingActive = true,
                pairingStatus = "starting",
                pairingError = null
            )
            try {
                val startRes = client.startPairing(deviceName = "Android TV Wohnzimmer")
                activePairingId = startRes.pairingId
                activePairingSecret = startRes.pairingSecret
                _uiState.value = _uiState.value.copy(
                    pairingCode = startRes.userCode,
                    pairingQrPayload = startRes.qrPayload,
                    pairingStatus = "pending"
                )

                // Start polling until approved or cancelled
                pollPairing(client, startRes.pairingId, startRes.pairingSecret)
            } catch (e: Exception) {
                _uiState.value = _uiState.value.copy(
                    pairingStatus = "error",
                    pairingError = "Kopplung konnte nicht gestartet werden: ${e.localizedMessage}"
                )
            }
        }
    }

    private fun pollPairing(
        client: io.github.manugh.xg2g.android.pairing.PairingApiClient,
        pairingId: String,
        pairingSecret: String
    ) {
        viewModelScope.launch {
            while (_uiState.value.isPairingActive && _uiState.value.pairingStatus == "pending") {
                kotlinx.coroutines.delay(2000)
                if (!_uiState.value.isPairingActive) break

                try {
                    val statusRes = client.getPairingStatus(pairingId, pairingSecret)
                    if (statusRes.status == "approved") {
                        _uiState.value = _uiState.value.copy(pairingStatus = "exchanging")
                        val exchangeRes = client.exchangePairing(pairingId, pairingSecret)
                        saveToken(exchangeRes.accessToken)
                        _uiState.value = _uiState.value.copy(
                            isPairingActive = false,
                            pairingStatus = "success",
                            message = "Gerät erfolgreich gekoppelt!"
                        )
                        break
                    } else if (statusRes.status == "expired" || statusRes.status == "revoked") {
                        _uiState.value = _uiState.value.copy(
                            pairingStatus = "error",
                            pairingError = "Kopplungs-Anfrage ist abgelaufen oder wurde abgelehnt."
                        )
                        break
                    }
                } catch (e: Exception) {
                    // Ignore transient network errors during polling
                }
            }
        }
    }

    fun cancelPairing() {
        _uiState.value = _uiState.value.copy(
            isPairingActive = false,
            pairingStatus = "idle",
            pairingCode = null,
            pairingError = null
        )
        activePairingId = null
        activePairingSecret = null
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
            val pairingClient = io.github.manugh.xg2g.android.pairing.PairingApiClient(serverUrl)
            return SettingsViewModel(
                serverSettingsStore = store,
                dashboardApiClient = client,
                pairingApiClient = pairingClient
            ) as T
        }
    }
}
