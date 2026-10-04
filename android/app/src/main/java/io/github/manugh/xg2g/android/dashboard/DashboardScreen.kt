package io.github.manugh.xg2g.android.dashboard

import android.view.KeyEvent
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.guide.GuideHealthStatus
import io.github.manugh.xg2g.android.transport.dashboard.DashboardRecordingItem
import io.github.manugh.xg2g.android.transport.dashboard.DashboardTimerItem

@Composable
internal fun DashboardScreen(
    state: DashboardScreenState,
    onOpenGuide: () -> Unit,
    onOpenRecordings: () -> Unit,
    onOpenTimers: () -> Unit,
    onOpenSettings: () -> Unit,
    modifier: Modifier = Modifier
) {
    val scrollState = rememberScrollState()

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(
                Brush.radialGradient(
                    colors = listOf(
                        colorResource(R.color.color_surface_panel),
                        colorResource(R.color.color_bg_base),
                        Color.Black
                    )
                )
            )
            .padding(horizontal = 32.dp, vertical = 24.dp)
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(scrollState),
            verticalArrangement = Arrangement.spacedBy(24.dp)
        ) {
            // Header Bar
            DashboardHeader(
                serverLabel = state.serverLabel,
                healthState = state.healthState
            )

            // Hero Section: Watch Live TV / Open Guide Primary Action
            DashboardHeroStage(
                onOpenGuide = onOpenGuide
            )

            // Quick Destinations Grid
            DashboardQuickDestinations(
                onOpenGuide = onOpenGuide,
                onOpenRecordings = onOpenRecordings,
                onOpenTimers = onOpenTimers,
                onOpenSettings = onOpenSettings
            )

            // Modular Section: Recent Recordings Preview
            RecordingsPreviewSection(
                recordingsState = state.recordingsState,
                onOpenRecordings = onOpenRecordings
            )

            // Modular Section: Active Timers Preview
            TimersPreviewSection(
                timersState = state.timersState,
                onOpenTimers = onOpenTimers
            )
        }
    }
}

@Composable
private fun DashboardHeader(
    serverLabel: String,
    healthState: ModuleState<GuideHealthStatus>
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Column {
            Text(
                text = stringResource(R.string.brand_console),
                style = MaterialTheme.typography.labelMedium,
                color = colorResource(R.color.color_live),
                fontWeight = FontWeight.Bold
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.dashboard_title),
                style = MaterialTheme.typography.headlineLarge,
                color = colorResource(R.color.color_text_primary),
                fontWeight = FontWeight.Bold
            )
        }

        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            // Health Chip
            when (healthState) {
                is ModuleState.Success -> {
                    val health = healthState.data
                    val isHealthy = health.receiverHealthy && health.epgHealthy
                    val badgeColor = if (isHealthy) colorResource(R.color.color_status_success) else colorResource(R.color.color_status_warning)
                    Surface(
                        shape = RoundedCornerShape(12.dp),
                        color = badgeColor.copy(alpha = 0.15f),
                        border = BorderStroke(1.dp, badgeColor.copy(alpha = 0.4f))
                    ) {
                        Row(
                            modifier = Modifier.padding(horizontal = 12.dp, vertical = 6.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(8.dp)
                        ) {
                            Box(modifier = Modifier.size(8.dp).background(badgeColor, CircleShape))
                            Text(
                                text = if (isHealthy) stringResource(R.string.dashboard_health_receiver_ready) else stringResource(R.string.dashboard_health_limited_epg),
                                style = MaterialTheme.typography.labelMedium,
                                color = badgeColor,
                                fontWeight = FontWeight.Bold
                            )
                        }
                    }
                }
                is ModuleState.Loading -> {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                }
                is ModuleState.Error -> {
                    val msg = healthState.message.orEmpty()
                    val isAuthError = msg.contains("401") || msg.contains("Auth") || msg.contains("Unauthorized")
                    val statusText = if (isAuthError) stringResource(R.string.dashboard_auth_required) else stringResource(R.string.dashboard_offline)
                    Text(
                        text = statusText,
                        style = MaterialTheme.typography.labelMedium,
                        color = colorResource(R.color.color_status_error),
                        fontWeight = FontWeight.Bold
                    )
                }
                else -> {
                    Text(stringResource(R.string.dashboard_offline), style = MaterialTheme.typography.labelMedium, color = colorResource(R.color.color_status_error))
                }

            }

            // Server Label
            Surface(
                shape = RoundedCornerShape(12.dp),
                color = colorResource(R.color.color_surface_panel_soft),
                border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
            ) {
                Text(
                    text = serverLabel,
                    style = MaterialTheme.typography.labelLarge,
                    color = colorResource(R.color.color_text_secondary),
                    modifier = Modifier.padding(horizontal = 14.dp, vertical = 6.dp)
                )
            }
        }
    }
}

@Composable
private fun DashboardHeroStage(
    onOpenGuide: () -> Unit
) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = MaterialTheme.shapes.large,
        color = colorResource(R.color.color_surface_shell_strong),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_base))
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(24.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = stringResource(R.string.dashboard_hero_kicker),
                    style = MaterialTheme.typography.labelMedium,
                    color = colorResource(R.color.color_action),
                    fontWeight = FontWeight.Bold
                )
                Spacer(modifier = Modifier.height(6.dp))
                Text(
                    text = stringResource(R.string.dashboard_hero_title),
                    style = MaterialTheme.typography.titleLarge,
                    color = colorResource(R.color.color_text_primary),
                    fontWeight = FontWeight.SemiBold
                )
                Spacer(modifier = Modifier.height(6.dp))
                Text(
                    text = stringResource(R.string.dashboard_hero_subtitle),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colorResource(R.color.color_text_secondary)
                )
            }

            Spacer(modifier = Modifier.width(24.dp))

            TvActionButton(
                label = stringResource(R.string.dashboard_action_open_guide),
                onClick = onOpenGuide,
                isPrimary = true
            )

        }
    }
}

@Composable
private fun DashboardQuickDestinations(
    onOpenGuide: () -> Unit,
    onOpenRecordings: () -> Unit,
    onOpenTimers: () -> Unit,
    onOpenSettings: () -> Unit
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(16.dp)
    ) {
        TvCardButton(
            title = stringResource(R.string.dashboard_card_guide_title),
            subtitle = stringResource(R.string.dashboard_card_guide_subtitle),
            onClick = onOpenGuide,
            modifier = Modifier.weight(1f)
        )
        TvCardButton(
            title = stringResource(R.string.dashboard_card_recordings_title),
            subtitle = stringResource(R.string.dashboard_card_recordings_subtitle),
            onClick = onOpenRecordings,
            modifier = Modifier.weight(1f)
        )
        TvCardButton(
            title = stringResource(R.string.dashboard_card_timers_title),
            subtitle = stringResource(R.string.dashboard_card_timers_subtitle),
            onClick = onOpenTimers,
            modifier = Modifier.weight(1f)
        )
        TvCardButton(
            title = stringResource(R.string.dashboard_card_settings_title),
            subtitle = stringResource(R.string.dashboard_card_settings_subtitle),
            onClick = onOpenSettings,
            modifier = Modifier.weight(1f)
        )
    }
}

@Composable
private fun RecordingsPreviewSection(
    recordingsState: ModuleState<List<DashboardRecordingItem>>,
    onOpenRecordings: () -> Unit
) {
    Column {
        Text(
            text = stringResource(R.string.dashboard_recordings_title),
            style = MaterialTheme.typography.titleMedium,
            color = colorResource(R.color.color_text_primary),
            fontWeight = FontWeight.SemiBold
        )
        Spacer(modifier = Modifier.height(12.dp))

        when (recordingsState) {
            is ModuleState.Success -> {
                val fallbackChannel = stringResource(R.string.dashboard_recordings_fallback_channel)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    items(recordingsState.data.take(5), key = { it.id }) { recording ->
                        TvItemCard(
                            title = recording.title,
                            subtitle = recording.channelName ?: fallbackChannel,
                            onClick = onOpenRecordings
                        )
                    }
                }
            }
            is ModuleState.Empty -> {
                Text(
                    text = stringResource(R.string.dashboard_recordings_empty),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colorResource(R.color.color_text_secondary)
                )
            }
            is ModuleState.Error -> {
                val msg = recordingsState.message.orEmpty()
                val isAuthError = msg.contains("401") || msg.contains("Auth") || msg.contains("Unauthorized")
                if (isAuthError) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(16.dp)
                    ) {
                        Text(
                            text = stringResource(R.string.dashboard_recordings_auth_required),
                            style = MaterialTheme.typography.bodyMedium,
                            color = colorResource(R.color.color_status_warning)
                        )
                        TvActionButton(
                            label = stringResource(R.string.dashboard_action_signin_now),
                            onClick = onOpenRecordings,
                            isPrimary = false
                        )
                    }
                } else {
                    Text(
                        text = stringResource(R.string.dashboard_recordings_error),
                        style = MaterialTheme.typography.bodyMedium,
                        color = colorResource(R.color.color_status_error)
                    )
                }
            }
            is ModuleState.Loading -> {
                CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp)
            }
        }
    }
}

@Composable
private fun TimersPreviewSection(
    timersState: ModuleState<List<DashboardTimerItem>>,
    onOpenTimers: () -> Unit
) {
    Column {
        Text(
            text = stringResource(R.string.dashboard_timers_title),
            style = MaterialTheme.typography.titleMedium,
            color = colorResource(R.color.color_text_primary),
            fontWeight = FontWeight.SemiBold
        )
        Spacer(modifier = Modifier.height(12.dp))

        when (timersState) {
            is ModuleState.Success -> {
                val fallbackChannel = stringResource(R.string.dashboard_timers_fallback_channel)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    items(timersState.data.take(5), key = { it.id }) { timer ->
                        TvItemCard(
                            title = timer.title,
                            subtitle = timer.channelName ?: fallbackChannel,
                            onClick = onOpenTimers
                        )
                    }
                }
            }
            is ModuleState.Empty -> {
                Text(
                    text = stringResource(R.string.dashboard_timers_empty),
                    style = MaterialTheme.typography.bodyMedium,
                    color = colorResource(R.color.color_text_secondary)
                )
            }
            is ModuleState.Error -> {
                val msg = timersState.message.orEmpty()
                val isAuthError = msg.contains("401") || msg.contains("Auth") || msg.contains("Unauthorized")
                if (isAuthError) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(16.dp)
                    ) {
                        Text(
                            text = stringResource(R.string.dashboard_timers_auth_required),
                            style = MaterialTheme.typography.bodyMedium,
                            color = colorResource(R.color.color_status_warning)
                        )
                        TvActionButton(
                            label = stringResource(R.string.dashboard_action_signin_now),
                            onClick = onOpenTimers,
                            isPrimary = false
                        )
                    }
                } else {
                    Text(
                        text = stringResource(R.string.dashboard_timers_error),
                        style = MaterialTheme.typography.bodyMedium,
                        color = colorResource(R.color.color_status_error)
                    )
                }
            }

            is ModuleState.Loading -> {
                CircularProgressIndicator(modifier = Modifier.size(24.dp), strokeWidth = 2.dp)
            }
        }
    }
}

@Composable
private fun TvActionButton(
    label: String,
    onClick: () -> Unit,
    isPrimary: Boolean = false
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    val scale by animateFloatAsState(if (isFocused) 1.05f else 1.0f, label = "btnScale")
    val actionColor = colorResource(R.color.color_action)

    val backgroundColor = when {
        isFocused -> actionColor
        isPrimary -> colorResource(R.color.color_bg_elevated)
        else -> colorResource(R.color.color_surface_panel_soft)
    }

    val borderColor = if (isFocused) Color.White else colorResource(R.color.color_border_base)

    Box(
        modifier = Modifier
            .scale(scale)
            .clip(RoundedCornerShape(12.dp))
            .background(backgroundColor)
            .border(2.dp, borderColor, RoundedCornerShape(12.dp))
            .focusable(interactionSource = interactionSource)
            .onKeyEvent { event ->
                if (event.nativeKeyEvent.action == KeyEvent.ACTION_DOWN &&
                    (event.nativeKeyEvent.keyCode == KeyEvent.KEYCODE_DPAD_CENTER || event.nativeKeyEvent.keyCode == KeyEvent.KEYCODE_ENTER)
                ) {
                    onClick()
                    true
                } else false
            }
            .padding(horizontal = 24.dp, vertical = 14.dp),
        contentAlignment = Alignment.Center
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
            color = Color.White
        )
    }
}

@Composable
private fun TvCardButton(
    title: String,
    subtitle: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    val scale by animateFloatAsState(if (isFocused) 1.04f else 1.0f, label = "cardScale")
    val actionColor = colorResource(R.color.color_action)

    val backgroundColor = if (isFocused) actionColor.copy(alpha = 0.25f) else colorResource(R.color.color_surface_shell_strong)
    val borderColor = if (isFocused) actionColor else colorResource(R.color.color_border_subtle)

    Column(
        modifier = modifier
            .scale(scale)
            .clip(RoundedCornerShape(12.dp))
            .background(backgroundColor)
            .border(if (isFocused) 2.dp else 1.dp, borderColor, RoundedCornerShape(12.dp))
            .focusable(interactionSource = interactionSource)
            .onKeyEvent { event ->
                if (event.nativeKeyEvent.action == KeyEvent.ACTION_DOWN &&
                    (event.nativeKeyEvent.keyCode == KeyEvent.KEYCODE_DPAD_CENTER || event.nativeKeyEvent.keyCode == KeyEvent.KEYCODE_ENTER)
                ) {
                    onClick()
                    true
                } else false
            }
            .padding(16.dp)
    ) {
        Text(
            text = title,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.Bold,
            color = if (isFocused) Color.White else colorResource(R.color.color_text_primary)
        )
        Spacer(modifier = Modifier.height(4.dp))
        Text(
            text = subtitle,
            style = MaterialTheme.typography.labelMedium,
            color = colorResource(R.color.color_text_secondary),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
    }
}

@Composable
private fun TvItemCard(
    title: String,
    subtitle: String,
    onClick: () -> Unit
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    val scale by animateFloatAsState(if (isFocused) 1.04f else 1.0f, label = "itemScale")
    val actionColor = colorResource(R.color.color_action)

    val backgroundColor = if (isFocused) actionColor.copy(alpha = 0.25f) else colorResource(R.color.color_surface_shell_strong)
    val borderColor = if (isFocused) actionColor else colorResource(R.color.color_border_subtle)

    Column(
        modifier = Modifier
            .width(200.dp)
            .scale(scale)
            .clip(RoundedCornerShape(12.dp))
            .background(backgroundColor)
            .border(if (isFocused) 2.dp else 1.dp, borderColor, RoundedCornerShape(12.dp))
            .focusable(interactionSource = interactionSource)
            .onKeyEvent { event ->
                if (event.nativeKeyEvent.action == KeyEvent.ACTION_DOWN &&
                    (event.nativeKeyEvent.keyCode == KeyEvent.KEYCODE_DPAD_CENTER || event.nativeKeyEvent.keyCode == KeyEvent.KEYCODE_ENTER)
                ) {
                    onClick()
                    true
                } else false
            }
            .padding(14.dp)
    ) {
        Text(
            text = title,
            style = MaterialTheme.typography.titleSmall,
            fontWeight = FontWeight.Bold,
            color = colorResource(R.color.color_text_primary),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
        Spacer(modifier = Modifier.height(4.dp))
        Text(
            text = subtitle,
            style = MaterialTheme.typography.labelSmall,
            color = colorResource(R.color.color_text_secondary),
            maxLines = 1,
            overflow = TextOverflow.Ellipsis
        )
    }
}
