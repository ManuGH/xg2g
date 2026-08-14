package io.github.manugh.xg2g.android.timers

import android.content.Context
import androidx.compose.runtime.Immutable
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import io.github.manugh.xg2g.android.dashboard.ModuleState
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

@Immutable
internal data class TimersUiState(
    val timersState: ModuleState<List<TimerItem>> = ModuleState.Loading,
    val baseUrl: String = "",
    val serverLabel: String = ""
)

internal class TimersViewModel(
    val baseUrl: String,
    val serverLabel: String,
    private val apiClient: TimersApiClient,
    private val authTokenProvider: () -> String?
) : ViewModel() {

    private val _uiState = MutableStateFlow(
        TimersUiState(
            baseUrl = baseUrl,
            serverLabel = serverLabel
        )
    )
    val uiState: StateFlow<TimersUiState> = _uiState.asStateFlow()

    init {
        refresh(isInitial = true)
    }

    fun refresh(isInitial: Boolean = false) {
        viewModelScope.launch {
            val currentState = _uiState.value
            if (isInitial && currentState.timersState !is ModuleState.Success) {
                _uiState.value = currentState.copy(timersState = ModuleState.Loading)
            }

            try {
                val currentToken = authTokenProvider()
                val timers = apiClient.fetchTimers(currentToken)
                _uiState.value = currentState.copy(
                    timersState = if (timers.isEmpty()) ModuleState.Empty else ModuleState.Success(timers)
                )
            } catch (e: Exception) {
                _uiState.value = currentState.copy(
                    timersState = ModuleState.Error(e.message ?: "Fehler beim Laden der Timer.")
                )
            }
        }
    }

    class Factory(
        private val context: Context,
        private val serverLabelProvider: () -> String,
        private val baseUrlProvider: () -> String,
        private val authTokenProvider: () -> String?,
        private val stateStore: io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore? = null,
        private val dpopProvider: io.github.manugh.xg2g.android.auth.DPoPProvider? = null,
        private val stateMachine: io.github.manugh.xg2g.android.auth.AuthStateMachine? = null
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            val deviceAuthStore = stateStore ?: io.github.manugh.xg2g.android.DeviceAuthStore(context.applicationContext)
            val dpop = dpopProvider ?: io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider()
            val client = TimersApiClient(
                baseUrlProvider = baseUrlProvider,
                stateStore = deviceAuthStore,
                dpopProvider = dpop,
                stateMachine = stateMachine
            )
            return TimersViewModel(
                baseUrl = baseUrlProvider(),
                serverLabel = serverLabelProvider(),
                apiClient = client,
                authTokenProvider = authTokenProvider
            ) as T
        }
    }
}
