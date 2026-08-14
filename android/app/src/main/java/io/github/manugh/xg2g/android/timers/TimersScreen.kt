package io.github.manugh.xg2g.android.timers

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.dashboard.ModuleState
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@Composable
internal fun TimersScreen(
    viewModel: TimersViewModel,
    onOpenSetup: (() -> Unit)? = null
) {
    val state by viewModel.uiState.collectAsState()
    val gridState = rememberLazyGridState()

    Surface(
        modifier = Modifier.fillMaxSize(),
        color = colorResource(R.color.color_bg_base)
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 28.dp, vertical = 20.dp)
        ) {
            // Header
            TimersHeader(
                serverLabel = state.serverLabel,
                onRefresh = { viewModel.refresh(isInitial = false) }
            )

            Spacer(modifier = Modifier.height(16.dp))

            when (val timersState = state.timersState) {
                is ModuleState.Loading -> {
                    Box(
                        modifier = Modifier.fillMaxSize(),
                        contentAlignment = Alignment.Center
                    ) {
                        CircularProgressIndicator(color = colorResource(R.color.color_action))
                    }
                }
                is ModuleState.Error -> {
                    TimersErrorView(
                        message = timersState.message.orEmpty(),
                        onRetry = { viewModel.refresh(isInitial = true) },
                        onOpenSetup = onOpenSetup
                    )
                }
                is ModuleState.Empty -> {
                    Box(
                        modifier = Modifier.fillMaxSize(),
                        contentAlignment = Alignment.Center
                    ) {
                        Text(
                            text = "Keine geplanten Timer auf dem Receiver vorhanden.",
                            style = MaterialTheme.typography.bodyLarge,
                            color = colorResource(R.color.color_text_secondary)
                        )
                    }
                }
                is ModuleState.Success -> {
                    val timers = timersState.data
                    var lastFocusedId by rememberSaveable { mutableStateOf<String?>(null) }
                    val focusRequesters = remember { mutableMapOf<String, FocusRequester>() }

                    LaunchedEffect(timers, lastFocusedId) {
                        lastFocusedId?.let { id ->
                            focusRequesters[id]?.requestFocus()
                        }
                    }

                    Text(
                        text = "GEPLANTE TIMER (${timers.size})",
                        style = MaterialTheme.typography.labelSmall,
                        color = colorResource(R.color.color_text_secondary),
                        fontWeight = FontWeight.Bold,
                        letterSpacing = 1.2.sp
                    )
                    Spacer(modifier = Modifier.height(10.dp))

                    LazyVerticalGrid(
                        columns = GridCells.Fixed(3),
                        state = gridState,
                        horizontalArrangement = Arrangement.spacedBy(16.dp),
                        verticalArrangement = Arrangement.spacedBy(16.dp),
                        contentPadding = PaddingValues(bottom = 32.dp),
                        modifier = Modifier.fillMaxSize()
                    ) {
                        items(timers, key = { it.timerId }) { item ->
                            val requester = focusRequesters.getOrPut(item.timerId) { FocusRequester() }
                            TimerCard(
                                item = item,
                                onFocused = { lastFocusedId = item.timerId },
                                focusRequester = requester
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun TimersHeader(
    serverLabel: String,
    onRefresh: () -> Unit
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Column {
            Text(
                text = "xg2g BROADCAST CONSOLE",
                style = MaterialTheme.typography.labelSmall,
                color = colorResource(R.color.color_text_secondary),
                letterSpacing = 1.5.sp
            )
            Text(
                text = "Geplante Timer",
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.Bold,
                color = colorResource(R.color.color_text_primary)
            )
        }

        if (serverLabel.isNotBlank()) {
            Surface(
                shape = RoundedCornerShape(20.dp),
                color = colorResource(R.color.color_bg_elevated),
                border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
            ) {
                Text(
                    text = serverLabel,
                    style = MaterialTheme.typography.bodySmall,
                    color = colorResource(R.color.color_text_secondary),
                    modifier = Modifier.padding(horizontal = 14.dp, vertical = 6.dp)
                )
            }
        }
    }
}

@Composable
private fun TimersErrorView(
    message: String,
    onRetry: () -> Unit,
    onOpenSetup: (() -> Unit)?
) {
    val isAuthError = message.contains("401") || message.contains("Auth") || message.contains("Unauthorized")

    Box(
        modifier = Modifier.fillMaxSize(),
        contentAlignment = Alignment.Center
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            Text(
                text = if (isAuthError) "Anmeldung erforderlich, um Timer abzudrucken." else message,
                style = MaterialTheme.typography.bodyMedium,
                color = colorResource(R.color.color_status_warning)
            )

            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                if (isAuthError && onOpenSetup != null) {
                    TvActionButton(
                        label = "Jetzt Anmelden",
                        onClick = onOpenSetup,
                        isPrimary = true
                    )
                } else {
                    TvActionButton(
                        label = "Erneut versuchen",
                        onClick = onRetry,
                        isPrimary = true
                    )
                }
            }
        }
    }
}

@Composable
private fun TimerCard(
    item: TimerItem,
    onFocused: (() -> Unit)? = null,
    focusRequester: FocusRequester? = null
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    LaunchedEffect(isFocused) {
        if (isFocused) {
            onFocused?.invoke()
        }
    }

    Surface(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .let { mod ->
                if (focusRequester != null) mod.focusRequester(focusRequester) else mod
            }
            .focusable(interactionSource = interactionSource),
        shape = RoundedCornerShape(12.dp),
        color = colorResource(R.color.color_bg_elevated),
        border = BorderStroke(
            width = if (isFocused) 2.dp else 1.dp,
            color = if (isFocused) colorResource(R.color.color_action) else colorResource(R.color.color_border_subtle)
        )
    ) {
        Column(
            modifier = Modifier.padding(16.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                // Channel Name
                Text(
                    text = item.serviceName ?: "Unbekannter Sender",
                    style = MaterialTheme.typography.labelSmall,
                    color = colorResource(R.color.color_live),
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f, fill = false)
                )

                Spacer(modifier = Modifier.width(8.dp))

                // Status Badge
                TimerStatusBadge(disabled = item.disabled, state = item.state)
            }

            Spacer(modifier = Modifier.height(6.dp))

            // Timer Title
            Text(
                text = item.title ?: "Geplante Aufnahme",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.Bold,
                color = colorResource(R.color.color_text_primary),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )

            Spacer(modifier = Modifier.height(6.dp))

            // Time & Date Range
            val timeRange = formatTimeRange(item.beginUnixSeconds, item.endUnixSeconds)
            Text(
                text = timeRange,
                style = MaterialTheme.typography.bodySmall,
                color = colorResource(R.color.color_text_secondary)
            )
        }
    }
}

@Composable
private fun TimerStatusBadge(disabled: Boolean, state: String?) {
    val (label, bg, fg) = when {
        disabled -> Triple("DEAKTIVIERT", Color(0xFF334155), Color(0xFF94A3B8))
        state?.lowercase() == "recording" -> Triple("NIMMT AUF", Color(0xFF7F1D1D), Color(0xFFEF4444))
        state?.lowercase() == "running" -> Triple("AKTIV", Color(0xFF065F46), Color(0xFF10B981))
        else -> Triple("GEPLANT", Color(0xFF1E293B), Color(0xFF38BDF8))
    }

    Surface(
        shape = RoundedCornerShape(4.dp),
        color = bg
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = fg,
            fontSize = 10.sp,
            fontWeight = FontWeight.Bold,
            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp)
        )
    }
}

@Composable
private fun TvActionButton(
    label: String,
    onClick: () -> Unit,
    isPrimary: Boolean
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    Surface(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick
            )
            .focusable(interactionSource = interactionSource),
        shape = RoundedCornerShape(8.dp),
        color = if (isPrimary) colorResource(R.color.color_action) else colorResource(R.color.color_bg_elevated),
        border = BorderStroke(
            width = if (isFocused) 2.dp else 1.dp,
            color = if (isFocused) Color.White else colorResource(R.color.color_border_subtle)
        )
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.SemiBold,
            color = Color.White,
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)
        )
    }
}

private fun formatTimeRange(startUnix: Long, endUnix: Long): String {
    if (startUnix <= 0L) return ""
    val sdfDate = SimpleDateFormat("EEE, dd.MM.yyyy", Locale.GERMAN)
    val sdfTime = SimpleDateFormat("HH:mm", Locale.GERMAN)
    val startDate = Date(startUnix * 1000L)
    val endDate = Date(endUnix * 1000L)

    val dateStr = sdfDate.format(startDate)
    val startTime = sdfTime.format(startDate)
    val endTime = sdfTime.format(endDate)

    return "$dateStr • $startTime - $endTime Uhr"
}
