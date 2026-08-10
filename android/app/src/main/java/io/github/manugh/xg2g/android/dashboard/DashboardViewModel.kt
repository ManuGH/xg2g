package io.github.manugh.xg2g.android.dashboard

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import io.github.manugh.xg2g.android.DeviceAuthRepository
import io.github.manugh.xg2g.android.guide.GuideHealthStatus
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

internal sealed class ModuleState<out T> {
    object Loading : ModuleState<Nothing>()
    data class Success<T>(val data: T) : ModuleState<T>()
    object Empty : ModuleState<Nothing>()
    data class Error(val message: String?) : ModuleState<Nothing>()
}

internal data class DashboardScreenState(
    val serverLabel: String,
    val healthState: ModuleState<GuideHealthStatus> = ModuleState.Loading,
    val recordingsState: ModuleState<List<DashboardRecordingItem>> = ModuleState.Loading,
    val timersState: ModuleState<List<DashboardTimerItem>> = ModuleState.Loading,
    val dvrState: ModuleState<DashboardDvrStatus> = ModuleState.Loading,
    val isRefreshing: Boolean = false
)

internal class DashboardViewModel(
    private val client: DashboardApiClient,
    private val serverLabel: String,
    private val authTokenProvider: () -> String?
) : ViewModel() {

    private val _state = MutableStateFlow(DashboardScreenState(serverLabel = serverLabel))
    val state: StateFlow<DashboardScreenState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        loadData()
    }

    private fun loadData() {
        val currentToken = authTokenProvider()

        viewModelScope.launch {
            try {
                val health = client.fetchHealth(currentToken)
                _state.value = _state.value.copy(healthState = ModuleState.Success(health))
            } catch (e: Exception) {
                _state.value = _state.value.copy(healthState = ModuleState.Error(e.message))
            }
        }

        viewModelScope.launch {
            try {
                val recordings = client.fetchRecordings(currentToken)
                _state.value = _state.value.copy(
                    recordingsState = if (recordings.isEmpty()) ModuleState.Empty else ModuleState.Success(recordings)
                )
            } catch (e: Exception) {
                _state.value = _state.value.copy(recordingsState = ModuleState.Error(e.message))
            }
        }

        viewModelScope.launch {
            try {
                val timers = client.fetchTimers(currentToken)
                _state.value = _state.value.copy(
                    timersState = if (timers.isEmpty()) ModuleState.Empty else ModuleState.Success(timers)
                )
            } catch (e: Exception) {
                _state.value = _state.value.copy(timersState = ModuleState.Error(e.message))
            }
        }

        viewModelScope.launch {
            try {
                val dvr = client.fetchDvrStatus(currentToken)
                _state.value = _state.value.copy(dvrState = ModuleState.Success(dvr))
            } catch (e: Exception) {
                _state.value = _state.value.copy(dvrState = ModuleState.Error(e.message))
            }
        }
    }

    class Factory(
        private val context: Context,
        private val serverLabel: String,
        private val baseUrl: String,
        private val authTokenProvider: () -> String?
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            val deviceAuthRepository = DeviceAuthRepository(context)
            val client = DashboardApiClient(
                baseUrl = baseUrl,
                deviceAuthRepository = deviceAuthRepository
            )
            return DashboardViewModel(client, serverLabel, authTokenProvider) as T
        }
    }
}
