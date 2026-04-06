package io.github.manugh.xg2g.android.guide

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

internal class GuideViewModel(
    private val serverLabel: String,
    private val repository: GuideRepository
) : ViewModel() {
    private val _state = MutableStateFlow<GuideScreenState>(GuideScreenState.Loading(serverLabel))
    val state: StateFlow<GuideScreenState> = _state.asStateFlow()

    private var loadJob: Job? = null

    init {
        refresh()
    }

    fun refresh() {
        val current = _state.value
        val selectedBouquet = when (current) {
            is GuideScreenState.Empty -> current.selectedBouquet
            is GuideScreenState.Ready -> current.selectedBouquet
            else -> ""
        }
        val knownBouquets = when (current) {
            is GuideScreenState.Empty -> current.bouquets
            is GuideScreenState.Ready -> current.bouquets
            else -> null
        }
        val selectedChannelRef = (current as? GuideScreenState.Ready)?.selectedChannelRef
        load(
            bouquetName = selectedBouquet,
            knownBouquets = knownBouquets,
            preferredChannelRef = selectedChannelRef
        )
    }

    fun selectBouquet(name: String) {
        val current = _state.value
        if (current is GuideScreenState.Ready && current.selectedBouquet == name) {
            return
        }
        val knownBouquets = when (current) {
            is GuideScreenState.Empty -> current.bouquets
            is GuideScreenState.Ready -> current.bouquets
            else -> emptyList()
        }
        load(
            bouquetName = name,
            knownBouquets = knownBouquets,
            preferredChannelRef = null
        )
    }

    fun selectChannel(serviceRef: String) {
        val current = _state.value as? GuideScreenState.Ready ?: return
        if (current.selectedChannelRef == serviceRef) {
            return
        }
        if (current.channels.none { it.serviceRef == serviceRef }) {
            return
        }
        _state.value = current.copy(selectedChannelRef = serviceRef)
    }

    private fun load(
        bouquetName: String,
        knownBouquets: List<GuideBouquet>?,
        preferredChannelRef: String?
    ) {
        loadJob?.cancel()
        val previous = _state.value
        _state.value = when (previous) {
            is GuideScreenState.Ready -> previous.copy(isRefreshing = true)
            else -> GuideScreenState.Loading(serverLabel)
        }
        loadJob = viewModelScope.launch {
            try {
                val content = if (knownBouquets == null && bouquetName.isBlank()) {
                    repository.loadInitial()
                } else {
                    repository.loadBouquet(
                        bouquetName = bouquetName,
                        knownBouquets = knownBouquets
                    )
                }
                val selectedChannelRef = content.channels
                    .firstOrNull { it.serviceRef == preferredChannelRef }
                    ?.serviceRef
                    ?: content.channels.firstOrNull()?.serviceRef
                _state.value = if (content.channels.isEmpty()) {
                    GuideScreenState.Empty(
                        serverLabel = serverLabel,
                        bouquets = content.bouquets,
                        selectedBouquet = content.selectedBouquet,
                        health = content.health,
                        timelineWindow = content.timelineWindow
                    )
                } else {
                    GuideScreenState.Ready(
                        serverLabel = serverLabel,
                        bouquets = content.bouquets,
                        selectedBouquet = content.selectedBouquet,
                        channels = content.channels,
                        selectedChannelRef = selectedChannelRef,
                        health = content.health,
                        timelineWindow = content.timelineWindow
                    )
                }
            } catch (error: Throwable) {
                _state.value = GuideScreenState.Error(
                    serverLabel = serverLabel,
                    detail = error.message.orEmpty(),
                    authRequired = error is GuideAuthRequiredException
                )
            }
        }
    }

    internal class Factory(
        private val serverLabel: String,
        private val baseUrl: String,
        private val authToken: String?
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            val repository = GuideRepository(
                apiClient = GuideApiClient(baseUrl),
                authToken = authToken
            )
            return GuideViewModel(
                serverLabel = serverLabel,
                repository = repository
            ) as T
        }
    }
}
