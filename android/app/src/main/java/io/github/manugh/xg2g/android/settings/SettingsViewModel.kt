package io.github.manugh.xg2g.android.settings

import android.content.Context
import androidx.compose.runtime.Immutable
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope

import io.github.manugh.xg2g.android.ServerSettingsStore
import io.github.manugh.xg2g.android.dashboard.DashboardApiClient
import io.github.manugh.xg2g.android.dashboard.NativeHouseholdProfile
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
    val pairingStatus: String = "idle",

    // Admin / Household PIN unlock
    val pinConfigured: Boolean = false,
    val isUnlocked: Boolean = false,
    val isUnlocking: Boolean = false,
    val unlockError: String? = null,

    // Household Profiles
    val profiles: List<NativeHouseholdProfile> = emptyList(),
    val selectedProfileId: String? = null,

    // Preferences
    val audioMode: String = "stereo",
    val dvrMode: String = "2h",

    // Channel Scan
    val scanState: String = "idle",
    val scannedChannels: Int = 0,
    val totalChannels: Int = 0,
    val updatedCount: Int = 0,
    val isScanTriggering: Boolean = false,
    val scanError: String? = null
)

internal class SettingsViewModel(
    private val serverSettingsStore: ServerSettingsStore,
    private val dashboardApiClient: DashboardApiClient,
    private val pairingApiClient: io.github.manugh.xg2g.android.pairing.PairingApiClient? = null
) : ViewModel() {

    private val _uiState = MutableStateFlow(
        SettingsUiState(
            serverUrl = serverSettingsStore.getServerUrl().orEmpty(),
            authToken = serverSettingsStore.getAuthToken(),
            audioMode = serverSettingsStore.getAudioMode(),
            dvrMode = serverSettingsStore.getDvrMode(),
            selectedProfileId = serverSettingsStore.getSelectedProfileId()
        )
    )
    val uiState: StateFlow<SettingsUiState> = _uiState.asStateFlow()

    private var activePairingSecret: String? = null
    private var activePairingId: String? = null

    init {
        refreshHealth()
        refreshUnlockStatus()
        refreshHouseholdProfiles()
        refreshScanStatus()
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

    fun refreshUnlockStatus() {
        viewModelScope.launch {
            val token = serverSettingsStore.getAuthToken()
            try {
                val status = dashboardApiClient.fetchHouseholdUnlockStatus(token)
                _uiState.value = _uiState.value.copy(
                    pinConfigured = status.pinConfigured,
                    isUnlocked = status.unlocked
                )
            } catch (e: Exception) {
                // Ignore errors, default to false
            }
        }
    }

    fun refreshHouseholdProfiles() {
        viewModelScope.launch {
            val token = serverSettingsStore.getAuthToken()
            try {
                val profiles = dashboardApiClient.fetchHouseholdProfiles(token)
                val storedId = serverSettingsStore.getSelectedProfileId()
                val activeId = storedId.takeIf { id -> profiles.any { it.id == id } } ?: profiles.firstOrNull()?.id
                if (activeId != storedId && activeId != null) {
                    serverSettingsStore.saveSelectedProfileId(activeId)
                }
                _uiState.value = _uiState.value.copy(
                    profiles = profiles,
                    selectedProfileId = activeId
                )
            } catch (e: Exception) {
                // Ignore transient errors
            }
        }
    }

    fun selectHouseholdProfile(profileId: String) {
        viewModelScope.launch {
            val target = _uiState.value.profiles.find { it.id == profileId }
            serverSettingsStore.saveSelectedProfileId(profileId)
            _uiState.value = _uiState.value.copy(
                selectedProfileId = profileId,
                message = "Profil \"${target?.name ?: profileId}\" aktiviert."
            )
        }
    }

    fun unlockWithPin(pin: String) {
        viewModelScope.launch {
            _uiState.value = _uiState.value.copy(isUnlocking = true, unlockError = null)
            val token = serverSettingsStore.getAuthToken()
            try {
                val res = dashboardApiClient.unlockHousehold(token, pin)
                if (res.unlocked) {
                    _uiState.value = _uiState.value.copy(
                        isUnlocked = true,
                        pinConfigured = res.pinConfigured,
                        isUnlocking = false,
                        unlockError = null,
                        message = "Admin-Modus freigeschaltet."
                    )
                } else {
                    _uiState.value = _uiState.value.copy(
                        isUnlocked = false,
                        isUnlocking = false,
                        unlockError = "Falscher Haushalt-PIN."
                    )
                }
            } catch (e: Exception) {
                _uiState.value = _uiState.value.copy(
                    isUnlocked = false,
                    isUnlocking = false,
                    unlockError = "Freischaltung fehlgeschlagen: ${e.localizedMessage}"
                )
            }
        }
    }

    fun lockAdminMode() {
        viewModelScope.launch {
            val token = serverSettingsStore.getAuthToken()
            try {
                dashboardApiClient.lockHousehold(token)
            } catch (e: Exception) {
                // Ignore
            } finally {
                _uiState.value = _uiState.value.copy(
                    isUnlocked = false,
                    message = "Admin-Modus gesperrt."
                )
            }
        }
    }

    fun saveAudioMode(mode: String) {
        serverSettingsStore.saveAudioMode(mode)
        _uiState.value = _uiState.value.copy(audioMode = mode)
    }

    fun saveDvrMode(mode: String) {
        serverSettingsStore.saveDvrMode(mode)
        _uiState.value = _uiState.value.copy(dvrMode = mode)
    }

    fun refreshScanStatus() {
        viewModelScope.launch {
            val token = serverSettingsStore.getAuthToken()
            try {
                val scan = dashboardApiClient.fetchScanStatus(token)
                _uiState.value = _uiState.value.copy(
                    scanState = scan.state,
                    scannedChannels = scan.scannedChannels,
                    totalChannels = scan.totalChannels,
                    updatedCount = scan.updatedCount,
                    scanError = scan.lastError
                )
            } catch (e: Exception) {
                // Ignore transient errors
            }
        }
    }

    fun triggerChannelScan() {
        viewModelScope.launch {
            _uiState.value = _uiState.value.copy(isScanTriggering = true, scanError = null)
            val token = serverSettingsStore.getAuthToken()
            try {
                dashboardApiClient.triggerSystemScan(token)
                _uiState.value = _uiState.value.copy(isScanTriggering = false, scanState = "running")
                refreshScanStatus()
            } catch (e: Exception) {
                _uiState.value = _uiState.value.copy(
                    isScanTriggering = false,
                    scanError = "Scan konnte nicht gestartet werden: ${e.localizedMessage}"
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
            refreshUnlockStatus()
            refreshScanStatus()
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
            val deviceAuthStore = io.github.manugh.xg2g.android.DeviceAuthStore(context.applicationContext)
            val dpopProvider = io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider()
            val client = DashboardApiClient(
                baseUrlProvider = { store.getServerUrl().orEmpty().ifBlank { serverUrl } },
                stateStore = deviceAuthStore,
                dpopProvider = dpopProvider
            )
            val pairingClient = io.github.manugh.xg2g.android.pairing.PairingApiClient(serverUrl)
            return SettingsViewModel(
                serverSettingsStore = store,
                dashboardApiClient = client,
                pairingApiClient = pairingClient
            ) as T
        }
    }
}
