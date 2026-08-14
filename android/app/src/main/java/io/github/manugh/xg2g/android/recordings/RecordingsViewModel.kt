package io.github.manugh.xg2g.android.recordings

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
internal data class RecordingsUiState(
    val recordingsState: ModuleState<List<RecordingItem>> = ModuleState.Loading,
    val continueWatchingState: ModuleState<List<RecordingItem>> = ModuleState.Empty,
    val selectedRoot: String? = null,
    val currentPath: String = "",
    val roots: List<RecordingRoot> = emptyList(),
    val directories: List<DirectoryItem> = emptyList(),
    val breadcrumbs: List<Breadcrumb> = emptyList(),
    val baseUrl: String = "",
    val serverLabel: String = ""
)

internal class RecordingsViewModel(
    val baseUrl: String,
    val serverLabel: String,
    private val apiClient: RecordingsApiClient,
    private val authTokenProvider: () -> String?
) : ViewModel() {

    private val _uiState = MutableStateFlow(
        RecordingsUiState(
            baseUrl = baseUrl,
            serverLabel = serverLabel
        )
    )
    val uiState: StateFlow<RecordingsUiState> = _uiState.asStateFlow()

    init {
        loadRecordings(root = null, path = null, isInitial = true)
    }

    fun refresh(isInitial: Boolean = false) {
        val current = _uiState.value
        loadRecordings(root = current.selectedRoot, path = current.currentPath, isInitial = isInitial)
    }

    fun selectRoot(rootId: String) {
        if (_uiState.value.selectedRoot == rootId && _uiState.value.currentPath.isEmpty()) return
        loadRecordings(root = rootId, path = "", isInitial = true)
    }

    fun navigateToDirectory(dirPath: String) {
        val root = _uiState.value.selectedRoot
        loadRecordings(root = root, path = dirPath, isInitial = true)
    }

    fun navigateToBreadcrumb(crumbPath: String) {
        val root = _uiState.value.selectedRoot
        loadRecordings(root = root, path = crumbPath, isInitial = true)
    }

    fun navigateUp() {
        val crumbs = _uiState.value.breadcrumbs
        if (crumbs.size > 1) {
            val parentCrumb = crumbs[crumbs.size - 2]
            navigateToBreadcrumb(parentCrumb.path)
        } else if (_uiState.value.currentPath.isNotEmpty()) {
            navigateToBreadcrumb("")
        }
    }

    private fun loadRecordings(root: String?, path: String?, isInitial: Boolean = false) {
        viewModelScope.launch {
            val currentState = _uiState.value
            if (isInitial) {
                _uiState.value = currentState.copy(recordingsState = ModuleState.Loading)
            }

            try {
                val currentToken = authTokenProvider()
                val response = apiClient.fetchRecordings(
                    authToken = currentToken,
                    root = root,
                    path = path
                )

                val continueItems = try {
                    if (path.isNullOrEmpty()) {
                        apiClient.fetchContinueWatching(currentToken)
                    } else {
                        emptyList()
                    }
                } catch (_: Exception) {
                    emptyList()
                }

                val validContinue = continueItems.filter { item ->
                    val r = item.resume
                    r != null && r.finished != true && r.posSeconds > 0
                }

                val effectiveRoot = response.currentRoot ?: root ?: response.roots.firstOrNull()?.id
                val effectivePath = response.currentPath ?: path.orEmpty()

                val recState = if (response.recordings.isEmpty()) {
                    ModuleState.Empty
                } else {
                    ModuleState.Success(response.recordings)
                }

                val contState = if (validContinue.isEmpty()) {
                    ModuleState.Empty
                } else {
                    ModuleState.Success(validContinue)
                }

                _uiState.value = _uiState.value.copy(
                    recordingsState = recState,
                    continueWatchingState = contState,
                    selectedRoot = effectiveRoot,
                    currentPath = effectivePath,
                    roots = response.roots,
                    directories = response.directories,
                    breadcrumbs = response.breadcrumbs
                )
            } catch (e: Exception) {
                if (currentState.recordingsState is ModuleState.Success) {
                    return@launch
                }
                _uiState.value = _uiState.value.copy(
                    recordingsState = ModuleState.Error(e.message ?: "Fehler beim Laden der Aufnahmen")
                )
            }
        }
    }

    class Factory(
        private val context: Context,
        private val serverLabelProvider: () -> String,
        private val baseUrlProvider: () -> String,
        private val authTokenProvider: () -> String?
    ) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            val deviceAuthStore = io.github.manugh.xg2g.android.DeviceAuthStore(context.applicationContext)
            val dpopProvider = io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider()
            val client = RecordingsApiClient(
                baseUrlProvider = baseUrlProvider,
                stateStore = deviceAuthStore,
                dpopProvider = dpopProvider
            )
            return RecordingsViewModel(
                baseUrl = baseUrlProvider(),
                serverLabel = serverLabelProvider(),
                apiClient = client,
                authTokenProvider = authTokenProvider
            ) as T
        }
    }
}
