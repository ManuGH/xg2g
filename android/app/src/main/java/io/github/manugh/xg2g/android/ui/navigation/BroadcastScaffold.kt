package io.github.manugh.xg2g.android.ui.navigation

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import io.github.manugh.xg2g.android.TvNavigationDestination

@Composable
internal fun BroadcastScaffold(
    currentDestination: TvNavigationDestination,
    onNavigate: (TvNavigationDestination) -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit
) {
    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
    ) {
        // Main Content Area: Fullscreen without artificial side-rail padding or layout reflows
        content()
    }
}
