package io.github.manugh.xg2g.android.guide

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.github.manugh.xg2g.android.R
import java.time.Instant
import java.time.ZoneId
import kotlinx.coroutines.delay

internal enum class GuideFocusedPane {
    BOUQUETS,
    CHANNELS
}

@Composable
internal fun GuideScreen(
    state: GuideScreenState,
    assetBaseUrl: String,
    onSelectBouquet: (String) -> Unit,
    onSelectChannel: (String) -> Unit,
    onRefresh: () -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onExit: () -> Unit
) {
    val referenceEpochSec = when (state) {
        is GuideScreenState.Empty -> state.referenceEpochSec
        is GuideScreenState.Ready -> state.referenceEpochSec
        else -> null
    }
    val displayZoneId = when (state) {
        is GuideScreenState.Empty -> guideDisplayZoneId(state.displayZoneOffsetSeconds)
        is GuideScreenState.Ready -> guideDisplayZoneId(state.displayZoneOffsetSeconds)
        else -> ZoneId.systemDefault()
    }
    val isRefreshing = (state as? GuideScreenState.Ready)?.isRefreshing ?: false

    val currentEpochSec by produceState(
        initialValue = referenceEpochSec ?: Instant.now().epochSecond,
        referenceEpochSec
    ) {
        if (referenceEpochSec == null) {
            while (true) {
                value = Instant.now().epochSecond
                delay(millisUntilNextProgressTick())
            }
        } else {
            var current = referenceEpochSec
            value = current
            while (true) {
                val tickMillis = millisUntilNextProgressTick()
                delay(tickMillis)
                current += (tickMillis / 1_000L).coerceAtLeast(1L)
                value = current
            }
        }
    }

    Box(
        modifier = Modifier
            .fillMaxSize()
            .background(
                Brush.radialGradient(
                    colors = listOf(
                        colorResource(R.color.color_surface_panel),
                        colorResource(R.color.color_bg_base),
                        Color(0F, 0F, 0F, 1F)
                    )
                )
            )
    ) {
        GuideBackdropArt()
        when (state) {
            is GuideScreenState.Loading -> GuideLoading(state.serverLabel)
            is GuideScreenState.Error -> GuideError(state, onRefresh)
            is GuideScreenState.Empty -> GuideContentLayout(
                bouquets = state.bouquets,
                selectedBouquet = state.selectedBouquet,
                channels = emptyList(),
                health = state.health,
                timelineWindow = state.timelineWindow,
                selectedChannelRef = null,
                currentEpochSec = currentEpochSec,
                displayZoneId = displayZoneId,
                assetBaseUrl = assetBaseUrl,
                isRefreshing = false,
                onSelectBouquet = onSelectBouquet,
                onSelectChannel = onSelectChannel,
                onPlayChannel = onPlayChannel,
                onRefresh = onRefresh,
                onExit = onExit
            )
            is GuideScreenState.Ready -> GuideContentLayout(
                bouquets = state.bouquets,
                selectedBouquet = state.selectedBouquet,
                channels = state.channels,
                health = state.health,
                timelineWindow = state.timelineWindow,
                selectedChannelRef = state.selectedChannelRef,
                currentEpochSec = currentEpochSec,
                displayZoneId = displayZoneId,
                assetBaseUrl = assetBaseUrl,
                isRefreshing = isRefreshing,
                onSelectBouquet = onSelectBouquet,
                onSelectChannel = onSelectChannel,
                onPlayChannel = onPlayChannel,
                onRefresh = onRefresh,
                onExit = onExit
            )
        }
    }
}

@Composable
internal fun BoxScope.GuideBackdropArt() {
    Image(
        painter = painterResource(R.drawable.xg2g_logo_mono_dark),
        contentDescription = null,
        contentScale = ContentScale.Fit,
        modifier = Modifier
            .align(Alignment.TopEnd)
            .padding(top = 8.dp, end = 8.dp)
            .width(240.dp)
            .alpha(0.08f)
    )
}

@Composable
internal fun GuideLoading(serverLabel: String) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = colorResource(R.color.color_surface_shell_strong),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_base)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Column(
            modifier = Modifier.padding(22.dp)
        ) {
            Text(
                text = stringResource(R.string.guide_loading),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold
            )
            Spacer(modifier = Modifier.height(10.dp))
            Text(
                text = stringResource(R.string.guide_loading_detail, serverLabel),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(16.dp))
            LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth(),
                color = MaterialTheme.colorScheme.primary,
                trackColor = MaterialTheme.colorScheme.surfaceVariant
            )
        }
    }
}

@Composable
internal fun GuideError(state: GuideScreenState.Error, onRefresh: () -> Unit) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = colorResource(R.color.color_surface_shell_strong),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_base)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Column(
            modifier = Modifier.padding(22.dp)
        ) {
            Text(
                text = if (state.authRequired) {
                    stringResource(R.string.guide_auth_title)
                } else {
                    stringResource(R.string.guide_error_title)
                },
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold
            )
            Spacer(modifier = Modifier.height(10.dp))
            Text(
                text = if (state.authRequired) {
                    state.detail.ifBlank { stringResource(R.string.guide_auth_detail) }
                } else {
                    state.detail.ifBlank { stringResource(R.string.guide_generic_detail) }
                },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(16.dp))
            OutlinedButton(onClick = onRefresh) {
                Text(stringResource(R.string.guide_refresh))
            }
        }
    }
}

@Composable
internal fun GuideContentLayout(
    bouquets: List<GuideBouquet>,
    selectedBouquet: String,
    channels: List<GuideChannel>,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    selectedChannelRef: String?,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    assetBaseUrl: String,
    isRefreshing: Boolean,
    onSelectBouquet: (String) -> Unit,
    onSelectChannel: (String) -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onRefresh: () -> Unit,
    onExit: () -> Unit
) {
    var focusedPane by remember { mutableStateOf(GuideFocusedPane.CHANNELS) }
    var activeDetailEvent by remember { mutableStateOf<Pair<GuideChannel, GuideProgram>?>(null) }

    BackHandler {
        if (activeDetailEvent != null) {
            activeDetailEvent = null
        } else {
            onExit()
        }
    }

    Box(
        modifier = Modifier.fillMaxSize()
    ) {
        ChannelListView(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            channels = channels,
            health = health,
            timelineWindow = timelineWindow,
            selectedChannelRef = selectedChannelRef,
            currentEpochSec = currentEpochSec,
            displayZoneId = displayZoneId,
            assetBaseUrl = assetBaseUrl,
            isRefreshing = isRefreshing,
            onSelectBouquet = onSelectBouquet,
            onSelectChannel = onSelectChannel,
            onPlayChannel = onPlayChannel,
            onOpenDetails = { channel, program ->
                activeDetailEvent = channel to program
            },
            onRefresh = onRefresh,
            onFocusedPane = { focusedPane = GuideFocusedPane.CHANNELS },
            modifier = Modifier.fillMaxSize()
        )

        activeDetailEvent?.let { (channel, program) ->
            EventDetailsDialog(
                channel = channel,
                program = program,
                currentEpochSec = currentEpochSec,
                displayZoneId = displayZoneId,
                onPlayChannel = {
                    activeDetailEvent = null
                    onPlayChannel(channel)
                },
                onDismiss = {
                    activeDetailEvent = null
                }
            )
        }
    }
}
