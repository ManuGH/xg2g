package io.github.manugh.xg2g.android.ui.navigation

import android.view.View
import androidx.compose.runtime.Composable
import androidx.compose.ui.viewinterop.AndroidView
import io.github.manugh.xg2g.android.TvNavigationDestination

@Composable
internal fun BroadcastApp(
    currentDestination: TvNavigationDestination,
    onNavigate: (TvNavigationDestination) -> Unit,
    dashboardScreen: @Composable () -> Unit,
    guideScreen: @Composable () -> Unit,
    recordingsScreen: @Composable () -> Unit,
    timersScreen: @Composable () -> Unit,
    settingsScreen: @Composable () -> Unit
) {
    BroadcastScaffold(
        currentDestination = currentDestination,
        onNavigate = onNavigate
    ) {
        when (currentDestination) {
            TvNavigationDestination.Home -> dashboardScreen()
            TvNavigationDestination.Guide -> guideScreen()
            TvNavigationDestination.Recordings -> recordingsScreen()
            TvNavigationDestination.Timers -> timersScreen()
            TvNavigationDestination.Settings -> settingsScreen()
        }
    }
}

