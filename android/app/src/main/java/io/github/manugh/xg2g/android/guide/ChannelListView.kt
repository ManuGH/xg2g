package io.github.manugh.xg2g.android.guide

import android.graphics.Bitmap
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ButtonDefaults
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.transport.guide.loadGuideBitmap
import io.github.manugh.xg2g.android.transport.guide.resolveGuideLogoUrl
import java.time.ZoneId

@Composable
internal fun ChannelListView(
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
    onOpenDetails: (GuideChannel, GuideProgram) -> Unit,
    onRefresh: () -> Unit,
    onFocusedPane: () -> Unit,
    modifier: Modifier = Modifier
) {
    val channelKeys = remember(channels) { channels.map(GuideChannel::serviceRef) }
    val channelRequesters = remember(channelKeys) {
        channelKeys.associateWith { FocusRequester() }
    }
    val bouquetRequesters = remember(bouquets) {
        bouquets.map { it.name }.associateWith { FocusRequester() }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp)
    ) {
        // TOP 1: Header Toolbar & Status Chips
        GuideHeader(
            serverLabel = "${channels.size} Sender",
            health = health,
            timelineWindow = timelineWindow,
            displayZoneId = displayZoneId,
            isRefreshing = isRefreshing,
            onRefresh = onRefresh
        )

        // TOP 2: Bouquet SurfaceTabs
        BouquetTabs(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            onSelectBouquet = onSelectBouquet,
            onFocusedPane = onFocusedPane,
            requesters = bouquetRequesters
        )

        // BODY: Channel Cards List (WebUI Card Aesthetic)
        if (channels.isEmpty()) {
            Surface(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(140.dp),
                shape = RoundedCornerShape(16.dp),
                color = colorResource(R.color.color_surface_panel),
                border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
            ) {
                Box(
                    modifier = Modifier.fillMaxSize(),
                    contentAlignment = Alignment.Center
                ) {
                    Text(
                        text = stringResource(R.string.guide_no_program),
                        style = MaterialTheme.typography.bodyLarge,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }
        } else {
            LazyColumn(
                modifier = Modifier.fillMaxSize(),
                verticalArrangement = Arrangement.spacedBy(10.dp),
                contentPadding = PaddingValues(bottom = 16.dp)
            ) {
                itemsIndexed(channels, key = { _, ch -> ch.serviceRef }) { _, channel ->
                    val requester = channelRequesters[channel.serviceRef]
                    val isSelected = channel.serviceRef == selectedChannelRef

                    WebUiChannelCard(
                        assetBaseUrl = assetBaseUrl,
                        channel = channel,
                        currentEpochSec = currentEpochSec,
                        displayZoneId = displayZoneId,
                        selected = isSelected,
                        modifier = Modifier
                            .fillMaxWidth()
                            .let { mod -> if (requester != null) mod.focusRequester(requester) else mod },
                        onFocus = {
                            onFocusedPane()
                            onSelectChannel(channel.serviceRef)
                        },
                        onPlay = { onPlayChannel(channel) },
                        onOpenDetails = { program -> onOpenDetails(channel, program) }
                    )
                }
            }
        }
    }
}

@Composable
internal fun WebUiChannelCard(
    assetBaseUrl: String,
    channel: GuideChannel,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    selected: Boolean,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit,
    onPlay: () -> Unit,
    onOpenDetails: (GuideProgram) -> Unit
) {
    var isFocused by remember { mutableStateOf(false) }
    val scale by animateFloatAsState(if (isFocused) 1.01f else 1f, label = "cardScale")
    val borderColor by animateColorAsState(
        targetValue = when {
            isFocused -> Color(0xFF00E5FF)
            selected -> Color(0x6600E5FF)
            else -> colorResource(R.color.color_border_subtle)
        },
        label = "cardBorder"
    )

    val primaryProgram = channelPrimaryProgram(channel, currentEpochSec)
    val isLive = primaryProgram != null && currentEpochSec in primaryProgram.startEpochSec until primaryProgram.endEpochSec
    val remainingMinutes = if (isLive) {
        ((primaryProgram.endEpochSec - currentEpochSec) / 60L).coerceAtLeast(1L)
    } else null

    val playButtonRequester = remember { FocusRequester() }
    val detailsButtonRequester = remember { FocusRequester() }

    Surface(
        modifier = modifier
            .scale(scale)
            .onFocusChanged {
                if (it.isFocused) {
                    isFocused = true
                    onFocus()
                } else if (!it.hasFocus) {
                    isFocused = false
                }
            }
            .onPreviewKeyEvent { event ->
                if (event.type == KeyEventType.KeyUp && event.key.isGuidePlayKey()) {
                    onPlay()
                    true
                } else {
                    false
                }
            }
            .focusable()
            .clickable(onClick = onPlay),
        shape = RoundedCornerShape(16.dp),
        color = if (isFocused) colorResource(R.color.color_surface_panel) else colorResource(R.color.color_surface_shell_strong),
        border = BorderStroke(if (isFocused) 2.dp else 1.dp, borderColor)
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            // LEFT: Channel Logo + Channel Number & Name
            Row(
                modifier = Modifier.width(220.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp)
            ) {
                GuideChannelLogo(
                    assetBaseUrl = assetBaseUrl,
                    channel = channel
                )
                Column(
                    verticalArrangement = Arrangement.spacedBy(2.dp)
                ) {
                    Text(
                        text = channel.displayName,
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        color = Color.White,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                    channelMeta(channel)?.let { meta ->
                        Text(
                            text = meta,
                            style = MaterialTheme.typography.labelSmall,
                            color = Color.White.copy(alpha = 0.6f)
                        )
                    }
                }
            }

            Spacer(modifier = Modifier.width(16.dp))

            // CENTER: Now & Next Program + Progress
            Column(
                modifier = Modifier.weight(1f),
                verticalArrangement = Arrangement.spacedBy(4.dp)
            ) {
                if (primaryProgram != null) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        if (isLive) {
                            Surface(
                                shape = RoundedCornerShape(6.dp),
                                color = colorResource(R.color.color_live).copy(alpha = 0.2f),
                                border = BorderStroke(1.dp, colorResource(R.color.color_live).copy(alpha = 0.5f))
                            ) {
                                Text(
                                    text = "● LIVE",
                                    modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp),
                                    style = MaterialTheme.typography.labelSmall,
                                    fontWeight = FontWeight.Bold,
                                    color = colorResource(R.color.color_live)
                                )
                            }
                        }

                        Text(
                            text = primaryProgram.title,
                            style = MaterialTheme.typography.bodyLarge,
                            fontWeight = FontWeight.Bold,
                            color = Color.White,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f, fill = false)
                        )

                        Text(
                            text = "${primaryProgram.displayStartTime(displayZoneId)} - ${primaryProgram.displayEndTime(displayZoneId)}",
                            style = MaterialTheme.typography.labelSmall,
                            color = Color.White.copy(alpha = 0.65f)
                        )

                        if (remainingMinutes != null) {
                            Text(
                                text = "· Noch ${remainingMinutes}m",
                                style = MaterialTheme.typography.labelSmall,
                                fontWeight = FontWeight.SemiBold,
                                color = Color(0xFF00E5FF)
                            )
                        }
                    }

                    // Progress bar
                    val totalSec = (primaryProgram.endEpochSec - primaryProgram.startEpochSec).coerceAtLeast(1L)
                    val elapsedSec = (currentEpochSec - primaryProgram.startEpochSec).coerceIn(0L, totalSec)
                    val progressFraction = if (isLive) elapsedSec.toFloat() / totalSec.toFloat() else 0f
                    if (isLive && progressFraction > 0f) {
                        LinearProgressIndicator(
                            progress = { progressFraction.coerceIn(0f, 1f) },
                            modifier = Modifier
                                .fillMaxWidth()
                                .height(4.dp)
                                .clip(RoundedCornerShape(2.dp)),
                            color = Color(0xFF00E5FF),
                            trackColor = Color(0x22FFFFFF)
                        )
                    }

                    // Next Program
                    if (channel.next != null) {
                        Text(
                            text = "DANACH: ${channel.next.title} (${channel.next.displayStartTime(displayZoneId)})",
                            style = MaterialTheme.typography.labelSmall,
                            color = Color.White.copy(alpha = 0.5f),
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis
                        )
                    }
                } else {
                    Text(
                        text = stringResource(R.string.guide_no_program),
                        style = MaterialTheme.typography.bodyMedium,
                        color = Color.White.copy(alpha = 0.5f)
                    )
                }
            }

            Spacer(modifier = Modifier.width(16.dp))

            // RIGHT: Action Buttons (Play & Details)
            Row(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                if (primaryProgram != null) {
                    OutlinedButton(
                        onClick = { onOpenDetails(primaryProgram) },
                        modifier = Modifier.focusRequester(detailsButtonRequester),
                        shape = RoundedCornerShape(12.dp),
                        colors = ButtonDefaults.outlinedButtonColors(
                            containerColor = colorResource(R.color.color_surface_panel_soft),
                            contentColor = Color.White
                        ),
                        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle)),
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 6.dp)
                    ) {
                        Text(
                            text = "Details",
                            style = MaterialTheme.typography.labelMedium
                        )
                    }
                }

                OutlinedButton(
                    onClick = onPlay,
                    modifier = Modifier.focusRequester(playButtonRequester),
                    shape = RoundedCornerShape(12.dp),
                    colors = ButtonDefaults.outlinedButtonColors(
                        containerColor = colorResource(R.color.color_action),
                        contentColor = Color.White
                    ),
                    border = BorderStroke(1.dp, colorResource(R.color.color_action)),
                    contentPadding = PaddingValues(horizontal = 14.dp, vertical = 6.dp)
                ) {
                    Text(
                        text = stringResource(R.string.guide_play),
                        style = MaterialTheme.typography.labelMedium,
                        fontWeight = FontWeight.Bold
                    )
                }
            }
        }
    }
}

@Composable
internal fun GuideChannelLogo(
    assetBaseUrl: String,
    channel: GuideChannel
) {
    val bitmap by produceState<Bitmap?>(initialValue = null, assetBaseUrl, channel.logoUrl) {
        val resolvedUrl = resolveGuideLogoUrl(assetBaseUrl, channel.logoUrl)
        value = if (resolvedUrl != null) {
            loadGuideBitmap(resolvedUrl)
        } else {
            null
        }
    }

    Surface(
        modifier = Modifier.size(52.dp),
        shape = RoundedCornerShape(12.dp),
        color = colorResource(R.color.color_surface_panel),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        if (bitmap != null) {
            Image(
                bitmap = bitmap!!.asImageBitmap(),
                contentDescription = channel.displayName,
                contentScale = ContentScale.Fit,
                modifier = Modifier
                    .fillMaxSize()
                    .padding(5.dp)
            )
        } else {
            Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.Center
            ) {
                Text(
                    text = channelLogoFallback(channel),
                    style = MaterialTheme.typography.labelLarge,
                    fontWeight = FontWeight.Bold,
                    color = colorResource(R.color.color_live)
                )
            }
        }
    }
}

internal fun channelPrimaryProgram(
    channel: GuideChannel,
    currentEpochSec: Long
): GuideProgram? {
    val liveProgram = channel.now?.takeIf { currentEpochSec < it.endEpochSec }
    if (liveProgram != null) {
        return liveProgram
    }
    if (channel.next != null) {
        return channel.next
    }
    return channel.schedule.firstOrNull { !it.description.isNullOrBlank() } ?: channel.schedule.firstOrNull()
}

internal fun normalizeGuideDescription(raw: String): String? =
    raw
        .replace("\\n", " ")
        .replace(Regex("\\s+"), " ")
        .trim()
        .takeIf { it.isNotEmpty() }

internal fun channelMeta(channel: GuideChannel): String? = buildList {
    channel.resolution?.takeIf { it.isNotBlank() }?.let(::add)
    channel.codec?.takeIf { it.isNotBlank() }?.uppercase()?.let(::add)
}.takeIf { it.isNotEmpty() }?.joinToString(" · ")

internal fun channelLogoFallback(channel: GuideChannel): String {
    channel.number?.trim()
        ?.takeIf { it.isNotEmpty() }
        ?.let { return it.take(3) }

    val initials = channel.name
        .split(' ', '-', '/', '.')
        .mapNotNull { part -> part.firstOrNull()?.uppercaseChar()?.toString() }
        .take(3)
        .joinToString("")

    return initials.ifBlank { "TV" }
}

