package io.github.manugh.xg2g.android


import android.view.View
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import io.github.manugh.xg2g.android.contract.RecordingItem
import io.github.manugh.xg2g.android.dashboard.DashboardScreen
import io.github.manugh.xg2g.android.dashboard.DashboardViewModel
import io.github.manugh.xg2g.android.guide.GuideScreen
import io.github.manugh.xg2g.android.guide.GuideViewModel
import io.github.manugh.xg2g.android.recordings.RecordingListItem
import io.github.manugh.xg2g.android.recordings.RecordingsScreen
import io.github.manugh.xg2g.android.recordings.RecordingsViewModel
import io.github.manugh.xg2g.android.settings.SettingsScreen
import io.github.manugh.xg2g.android.settings.SettingsViewModel
import io.github.manugh.xg2g.android.timers.TimersScreen
import io.github.manugh.xg2g.android.timers.TimersViewModel
import io.github.manugh.xg2g.android.ui.navigation.BroadcastApp
import io.github.manugh.xg2g.android.ui.theme.GuideTheme
import kotlinx.coroutines.flow.StateFlow

@Composable
internal fun MainActivityContent(
    destinationFlow: StateFlow<TvNavigationDestination>,
    onNavigate: (TvNavigationDestination) -> Unit,
    dashboardViewModel: DashboardViewModel,
    guideViewModel: GuideViewModel,
    recordingsViewModel: RecordingsViewModel,
    timersViewModel: TimersViewModel,
    settingsViewModel: SettingsViewModel,
    assetBaseUrl: String,
    onPlayChannel: (io.github.manugh.xg2g.android.guide.GuideChannel) -> Unit,
    onPlayRecording: (RecordingListItem) -> Unit,
    onOpenSetup: (() -> Unit)?,
    onExitGuide: () -> Unit
) {
    val currentDestination by destinationFlow.collectAsState()
    val dashboardState by dashboardViewModel.state.collectAsState()
    val guideState by guideViewModel.state.collectAsState()

    GuideTheme {
        BroadcastApp(
            currentDestination = currentDestination,
            onNavigate = onNavigate,
            dashboardScreen = {
                DashboardScreen(
                    state = dashboardState,
                    onOpenGuide = { onNavigate(TvNavigationDestination.Guide) },
                    onOpenRecordings = { onNavigate(TvNavigationDestination.Recordings) },
                    onOpenTimers = { onNavigate(TvNavigationDestination.Timers) },
                    onOpenSettings = { onNavigate(TvNavigationDestination.Settings) }
                )
            },
            guideScreen = {
                GuideScreen(
                    state = guideState,
                    assetBaseUrl = assetBaseUrl,
                    onSelectBouquet = guideViewModel::selectBouquet,
                    onSelectChannel = guideViewModel::selectChannel,
                    onRefresh = guideViewModel::refresh,
                    onPlayChannel = onPlayChannel,
                    onExit = onExitGuide
                )
            },
            recordingsScreen = {
                RecordingsScreen(
                    viewModel = recordingsViewModel,
                    onPlayRecording = onPlayRecording,
                    onOpenSetup = onOpenSetup
                )
            },
            timersScreen = {
                TimersScreen(
                    viewModel = timersViewModel,
                    onOpenSetup = onOpenSetup
                )
            },
            settingsScreen = {
                SettingsScreen(
                    viewModel = settingsViewModel,
                    onChangeServer = { onOpenSetup?.invoke() },
                    onBack = { onNavigate(TvNavigationDestination.Home) }
                )
            }
        )
    }
}

