package io.github.manugh.xg2g.android.guide

import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Bundle
import android.webkit.CookieManager
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.appcompat.app.AppCompatActivity
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import java.net.HttpURLConnection
import java.net.URL
import java.time.Instant
import java.time.ZoneId
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import kotlin.math.max

class GuideActivity : AppCompatActivity() {
    private lateinit var baseUrl: String
    private var authToken: String? = null
    private val playbackBridge by lazy(LazyThreadSafetyMode.NONE) { NativePlaybackBridge(this) }
    private val viewModel: GuideViewModel by viewModels {
        GuideViewModel.Factory(
            context = applicationContext,
            serverLabel = describeServer(baseUrl),
            baseUrl = baseUrl,
            authToken = authToken
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        baseUrl = intent.getStringExtra(EXTRA_BASE_URL).orEmpty()
        authToken = intent.getStringExtra(EXTRA_AUTH_TOKEN)?.trim()?.takeIf { it.isNotEmpty() }
        if (baseUrl.isBlank()) {
            finish()
            return
        }

        super.onCreate(savedInstanceState)

        setContent {
            val state by viewModel.state.collectAsStateWithLifecycle()
            GuideTheme {
                GuideScreen(
                    state = state,
                    assetBaseUrl = baseUrl,
                    onSelectBouquet = viewModel::selectBouquet,
                    onSelectChannel = viewModel::selectChannel,
                    onRefresh = viewModel::refresh,
                    onPlayChannel = ::playChannel,
                    onExit = ::finish
                )
            }
        }
    }

    private fun playChannel(channel: GuideChannel) {
        playbackBridge.start(
            NativePlaybackRequest.Live(
                serviceRef = channel.serviceRef,
                title = channel.displayName,
                logoUrl = channel.logoUrl,
                authToken = authToken
            )
        )
    }

    private fun describeServer(url: String): String {
        val uri = runCatching { Uri.parse(url) }.getOrNull()
        val host = uri?.host ?: return url
        val path = uri.path?.trim('/').orEmpty()
        return if (path.isNotBlank()) "$host/$path" else host
    }

    companion object {
        private const val EXTRA_BASE_URL = "guide_base_url"
        private const val EXTRA_AUTH_TOKEN = "guide_auth_token"

        fun createIntent(
            context: Context,
            baseUrl: String,
            authToken: String?
        ): Intent = Intent(context, GuideActivity::class.java).apply {
            putExtra(EXTRA_BASE_URL, baseUrl)
            authToken?.takeIf { it.isNotBlank() }?.let { putExtra(EXTRA_AUTH_TOKEN, it) }
        }
    }
}

private enum class GuideFocusedPane {
    BOUQUETS,
    CHANNELS
}

@Composable
private fun GuideTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = MaterialTheme.colorScheme.copy(
            primary = colorFromRes(R.color.ide_blue),
            secondary = colorFromRes(R.color.ide_live),
            surface = colorFromRes(R.color.ide_surface),
            surfaceVariant = colorFromRes(R.color.ide_surface_strong),
            background = colorFromRes(R.color.ide_background),
            onBackground = colorFromRes(R.color.ide_text_primary),
            onSurface = colorFromRes(R.color.ide_text_primary),
            onSurfaceVariant = colorFromRes(R.color.ide_text_secondary),
            outline = colorFromRes(R.color.ide_outline),
            outlineVariant = colorFromRes(R.color.ide_outline_soft),
            error = colorFromRes(R.color.ide_error)
        ),
        content = content
    )
}

@Composable
private fun GuideScreen(
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
                Brush.verticalGradient(
                    colors = listOf(
                        colorFromRes(R.color.ide_background),
                        colorFromRes(R.color.ide_surface_panel),
                        colorFromRes(R.color.ide_background)
                    )
                )
            )
            .padding(horizontal = 20.dp, vertical = 18.dp)
    ) {
        GuideBackdropArt()
        Column(
            modifier = Modifier.fillMaxSize()
        ) {
            GuideHeader(
                serverLabel = state.serverLabel,
                health = when (state) {
                    is GuideScreenState.Empty -> state.health
                    is GuideScreenState.Ready -> state.health
                    else -> null
                },
                timelineWindow = when (state) {
                    is GuideScreenState.Empty -> state.timelineWindow
                    is GuideScreenState.Ready -> state.timelineWindow
                    else -> null
                },
                displayZoneId = displayZoneId,
                isRefreshing = state is GuideScreenState.Ready && state.isRefreshing,
                onRefresh = onRefresh
            )
            Spacer(modifier = Modifier.height(18.dp))

            when (state) {
                is GuideScreenState.Loading -> GuideLoading(state.serverLabel)
                is GuideScreenState.Error -> GuideError(state)
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
                    onSelectBouquet = onSelectBouquet,
                    onSelectChannel = onSelectChannel,
                    onPlayChannel = onPlayChannel,
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
                    onSelectBouquet = onSelectBouquet,
                    onSelectChannel = onSelectChannel,
                    onPlayChannel = onPlayChannel,
                    onExit = onExit
                )
            }
        }
    }
}

@Composable
private fun BoxScope.GuideBackdropArt() {
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
private fun GuideHeader(
    serverLabel: String,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    displayZoneId: ZoneId,
    isRefreshing: Boolean,
    onRefresh: () -> Unit
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Column(
            modifier = Modifier.weight(1f)
        ) {
            Text(
                text = stringResource(R.string.guide_kicker),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.secondary
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.guide_title),
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onBackground
            )
            Spacer(modifier = Modifier.height(6.dp))
            Text(
                text = stringResource(R.string.guide_support),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(10.dp))
            Row(
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                GuideServerChip(serverLabel)
                GuideWindowChip(timelineWindow, displayZoneId)
                GuideHealthChip(health)
            }
        }

        Spacer(modifier = Modifier.width(16.dp))

        Column(
            horizontalAlignment = Alignment.End
        ) {
            OutlinedButton(
                onClick = onRefresh,
                modifier = Modifier.focusProperties { canFocus = false },
                shape = RoundedCornerShape(16.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    containerColor = colorFromRes(R.color.ide_surface_panel_soft),
                    contentColor = MaterialTheme.colorScheme.onSurface
                ),
                border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft))
            ) {
                Text(stringResource(R.string.guide_refresh))
            }
            if (isRefreshing) {
                Spacer(modifier = Modifier.height(10.dp))
                LinearProgressIndicator(
                    modifier = Modifier.width(150.dp),
                    color = MaterialTheme.colorScheme.primary,
                    trackColor = MaterialTheme.colorScheme.surfaceVariant
                )
            }
        }
    }
}

@Composable
private fun GuideWindowChip(timelineWindow: GuideTimelineWindow?, displayZoneId: ZoneId) {
    if (timelineWindow == null) {
        return
    }

    Surface(
        shape = RoundedCornerShape(14.dp),
        color = colorFromRes(R.color.ide_surface_panel_soft),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Text(
            text = stringResource(
                R.string.guide_window_label,
                formatGuideEpochTime(timelineWindow.startEpochSec, displayZoneId),
                formatGuideEpochTime(timelineWindow.endEpochSec, displayZoneId)
            ),
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
    }
}

@Composable
private fun GuideHealthChip(health: GuideHealthStatus?) {
    if (health == null) {
        return
    }

    val (labelRes, tone) = when {
        !health.receiverHealthy -> R.string.guide_health_receiver_issue to colorFromRes(R.color.ide_error)
        health.epgHealthy -> R.string.guide_health_epg_ready to colorFromRes(R.color.ide_live)
        else -> R.string.guide_health_epg_limited to colorFromRes(R.color.ide_live)
    }

    val text = if (!health.epgHealthy && (health.missingChannels ?: 0) > 0) {
        stringResource(labelRes) + " · " + stringResource(
            R.string.guide_health_missing_channels,
            health.missingChannels ?: 0
        )
    } else {
        stringResource(labelRes)
    }

    Surface(
        shape = RoundedCornerShape(14.dp),
        color = tone.copy(alpha = 0.14f),
        border = BorderStroke(1.dp, tone.copy(alpha = 0.35f)),
        contentColor = tone
    ) {
        Text(
            text = text,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            style = MaterialTheme.typography.labelLarge,
            color = tone
        )
    }
}

@Composable
private fun GuideServerChip(serverLabel: String) {
    Surface(
        shape = RoundedCornerShape(14.dp),
        color = colorFromRes(R.color.ide_surface_panel_soft),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Text(
            text = serverLabel,
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 8.dp),
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant
        )
    }
}

@Composable
private fun GuideLoading(serverLabel: String) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(22.dp),
        color = colorFromRes(R.color.ide_surface_strong),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline)),
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
private fun GuideError(state: GuideScreenState.Error) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(22.dp),
        color = colorFromRes(R.color.ide_surface_strong),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline)),
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
        }
    }
}

@Composable
private fun GuideContentLayout(
    bouquets: List<GuideBouquet>,
    selectedBouquet: String,
    channels: List<GuideChannel>,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    selectedChannelRef: String?,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    assetBaseUrl: String,
    onSelectBouquet: (String) -> Unit,
    onSelectChannel: (String) -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onExit: () -> Unit
) {
    val bouquetKeys = remember(bouquets) { bouquets.map(GuideBouquet::name) }
    val channelKeys = remember(channels) { channels.map(GuideChannel::serviceRef) }
    val bouquetControlRequester = remember { FocusRequester() }
    val bouquetRequesters = remember(bouquetKeys) {
        bouquetKeys.associateWith { FocusRequester() }
    }
    val channelRequesters = remember(channelKeys) {
        channelKeys.associateWith { FocusRequester() }
    }
    val bouquetListState = rememberLazyListState()
    val channelListState = rememberLazyListState()
    val selectedBouquetRequester = bouquetRequesters[selectedBouquet]
        ?: bouquetKeys.firstOrNull()?.let(bouquetRequesters::get)
    val selectedChannelKey = selectedChannelRef
        ?: channelKeys.firstOrNull()
    val selectedChannelRequester = selectedChannelKey?.let(channelRequesters::get)
    var focusedPane by remember { mutableStateOf(GuideFocusedPane.CHANNELS) }
    var bouquetPickerOpen by remember { mutableStateOf(false) }

    LaunchedEffect(selectedBouquet, bouquetKeys) {
        val index = bouquetKeys.indexOf(selectedBouquet)
        if (index >= 0) {
            bouquetListState.scrollToItem(index)
        }
    }

    LaunchedEffect(selectedChannelKey, channelKeys, bouquetPickerOpen) {
        if (bouquetPickerOpen) {
            return@LaunchedEffect
        }
        if (channelKeys.isEmpty()) {
            focusedPane = GuideFocusedPane.BOUQUETS
            return@LaunchedEffect
        }
        val index = channelKeys.indexOf(selectedChannelKey)
        if (index >= 0) {
            channelListState.scrollToItem(max(0, index - 1))
        }
        selectedChannelRequester?.requestFocus()
        focusedPane = GuideFocusedPane.CHANNELS
    }

    LaunchedEffect(bouquetPickerOpen, selectedBouquetRequester) {
        if (bouquetPickerOpen) {
            selectedBouquetRequester?.requestFocus()
            focusedPane = GuideFocusedPane.BOUQUETS
        }
    }

    BackHandler {
        if (bouquetPickerOpen) {
            bouquetPickerOpen = false
            bouquetControlRequester.requestFocus()
            focusedPane = GuideFocusedPane.BOUQUETS
        } else {
            onExit()
        }
    }

    Box(
        modifier = Modifier.fillMaxSize()
    ) {
        ChannelPane(
            selectedBouquet = selectedBouquet,
            assetBaseUrl = assetBaseUrl,
            channels = channels,
            health = health,
            timelineWindow = timelineWindow,
            selectedChannelRef = selectedChannelRef,
            currentEpochSec = currentEpochSec,
            displayZoneId = displayZoneId,
            listState = channelListState,
            requesters = channelRequesters,
            bouquetControlRequester = bouquetControlRequester,
            selectedChannelRequester = selectedChannelRequester,
            onOpenBouquetPicker = {
                bouquetPickerOpen = true
                focusedPane = GuideFocusedPane.BOUQUETS
            },
            onSelectChannel = onSelectChannel,
            onPlayChannel = onPlayChannel,
            onFocusedPane = { focusedPane = GuideFocusedPane.CHANNELS },
            modifier = Modifier.fillMaxSize()
        )
        if (bouquetPickerOpen) {
            BouquetPickerOverlay(
                bouquets = bouquets,
                selectedBouquet = selectedBouquet,
                listState = bouquetListState,
                requesters = bouquetRequesters,
                selectedChannelRequester = selectedChannelRequester,
                onSelectBouquet = { bouquet ->
                    bouquetPickerOpen = false
                    onSelectBouquet(bouquet)
                },
                onClose = {
                    bouquetPickerOpen = false
                    selectedChannelRequester?.requestFocus() ?: bouquetControlRequester.requestFocus()
                },
                onFocusedPane = { focusedPane = GuideFocusedPane.BOUQUETS },
                modifier = Modifier
                    .width(286.dp)
                    .fillMaxHeight()
            )
        }
    }
}

@Composable
private fun BouquetPickerOverlay(
    bouquets: List<GuideBouquet>,
    selectedBouquet: String,
    listState: androidx.compose.foundation.lazy.LazyListState,
    requesters: Map<String, FocusRequester>,
    selectedChannelRequester: FocusRequester?,
    onSelectBouquet: (String) -> Unit,
    onClose: () -> Unit,
    onFocusedPane: () -> Unit,
    modifier: Modifier = Modifier
) {
    Surface(
        modifier = modifier.padding(end = 14.dp),
        shape = RoundedCornerShape(22.dp),
        color = colorFromRes(R.color.ide_surface_panel),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(14.dp)
        ) {
            Text(
                text = stringResource(R.string.guide_bouquets),
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold
            )
            Spacer(modifier = Modifier.height(10.dp))
            LazyColumn(
                state = listState,
                verticalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                items(bouquets, key = { it.name }) { bouquet ->
                    val selected = bouquet.name == selectedBouquet
                    val requester = requesters.getValue(bouquet.name)
                    OutlinedButton(
                        onClick = { onSelectBouquet(bouquet.name) },
                        modifier = Modifier
                            .fillMaxWidth()
                            .focusRequester(requester)
                            .focusProperties {
                                right = selectedChannelRequester ?: FocusRequester.Default
                            }
                            .onPreviewKeyEvent { event ->
                                if (event.type == KeyEventType.KeyDown && event.key == Key.DirectionRight) {
                                    onClose()
                                    true
                                } else {
                                    false
                                }
                            }
                            .onFocusChanged {
                                if (it.isFocused) {
                                    onFocusedPane()
                                }
                            },
                        shape = RoundedCornerShape(18.dp),
                        colors = ButtonDefaults.outlinedButtonColors(
                            containerColor = if (selected) {
                                colorFromRes(R.color.ide_blue)
                            } else {
                                colorFromRes(R.color.ide_surface_panel_soft)
                            },
                            contentColor = colorFromRes(R.color.ide_text_primary)
                        ),
                        border = BorderStroke(
                            1.dp,
                            if (selected) colorFromRes(R.color.ide_blue) else colorFromRes(R.color.ide_outline_soft)
                        )
                    ) {
                        Column(
                            modifier = Modifier.fillMaxWidth()
                        ) {
                            Text(
                                text = bouquet.name,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis
                            )
                            if (bouquet.services > 0) {
                                Spacer(modifier = Modifier.height(4.dp))
                                Text(
                                    text = stringResource(R.string.guide_channels, bouquet.services),
                                    style = MaterialTheme.typography.labelMedium,
                                    color = if (selected) {
                                        colorFromRes(R.color.ide_text_primary)
                                    } else {
                                        MaterialTheme.colorScheme.onSurfaceVariant
                                    }
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ChannelPane(
    selectedBouquet: String,
    assetBaseUrl: String,
    channels: List<GuideChannel>,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    selectedChannelRef: String?,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    listState: androidx.compose.foundation.lazy.LazyListState,
    requesters: Map<String, FocusRequester>,
    bouquetControlRequester: FocusRequester,
    selectedChannelRequester: FocusRequester?,
    onOpenBouquetPicker: () -> Unit,
    onSelectChannel: (String) -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onFocusedPane: () -> Unit,
    modifier: Modifier = Modifier
) {
    val selectedChannel = channels.firstOrNull { it.serviceRef == selectedChannelRef }
    Surface(
        modifier = modifier.fillMaxHeight(),
        shape = RoundedCornerShape(22.dp),
        color = colorFromRes(R.color.ide_surface_strong),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(start = 14.dp, end = 14.dp, top = 14.dp),
                verticalAlignment = Alignment.Bottom
            ) {
                BouquetSelectorButton(
                    selectedBouquet = selectedBouquet,
                    onOpenBouquetPicker = onOpenBouquetPicker,
                    onFocusedPane = onFocusedPane,
                    channelFocusRequester = selectedChannelRequester,
                    modifier = Modifier.focusRequester(bouquetControlRequester)
                )
                Spacer(modifier = Modifier.width(14.dp))
                Column(
                    modifier = Modifier.weight(1f)
                ) {
                    Text(
                        text = stringResource(R.string.guide_channels, channels.size),
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }
            selectedChannel?.let { channel ->
                GuideSelectionPanel(
                    channel = channel,
                    currentEpochSec = currentEpochSec,
                    displayZoneId = displayZoneId,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(start = 14.dp, end = 14.dp, top = 12.dp)
                )
            }
            Spacer(modifier = Modifier.height(10.dp))
            if (channels.isEmpty()) {
                Column(
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(horizontal = 22.dp),
                    verticalArrangement = Arrangement.Center
                ) {
                    Text(
                        text = stringResource(R.string.guide_empty_title),
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.SemiBold
                    )
                    Spacer(modifier = Modifier.height(10.dp))
                    Text(
                        text = stringResource(R.string.guide_empty_detail),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            } else {
                LazyColumn(
                    state = listState,
                    modifier = Modifier
                        .fillMaxSize()
                        .padding(horizontal = 14.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp)
                ) {
                    itemsIndexed(channels, key = { _, channel -> channel.serviceRef }) { _, channel ->
                        ChannelCard(
                            assetBaseUrl = assetBaseUrl,
                            channel = channel,
                            health = health,
                            timelineWindow = timelineWindow,
                            currentEpochSec = currentEpochSec,
                            displayZoneId = displayZoneId,
                            selected = channel.serviceRef == selectedChannelRef,
                            modifier = Modifier
                                .focusRequester(requesters.getValue(channel.serviceRef))
                                .focusProperties {
                                    left = bouquetControlRequester
                                },
                            onFocus = {
                                onFocusedPane()
                                onSelectChannel(channel.serviceRef)
                            },
                            onPlayChannel = {
                                onSelectChannel(channel.serviceRef)
                                onPlayChannel(channel)
                            }
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun BouquetSelectorButton(
    selectedBouquet: String,
    onOpenBouquetPicker: () -> Unit,
    onFocusedPane: () -> Unit,
    channelFocusRequester: FocusRequester?,
    modifier: Modifier = Modifier
) {
    OutlinedButton(
        onClick = onOpenBouquetPicker,
        modifier = modifier
            .focusProperties {
                right = channelFocusRequester ?: FocusRequester.Default
            }
            .onFocusChanged {
                if (it.isFocused) {
                    onFocusedPane()
                }
            },
        shape = RoundedCornerShape(18.dp),
        colors = ButtonDefaults.outlinedButtonColors(
            containerColor = colorFromRes(R.color.ide_surface_panel_soft),
            contentColor = colorFromRes(R.color.ide_text_primary)
        ),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft))
    ) {
        Column(
            horizontalAlignment = Alignment.Start
        ) {
            Text(
                text = stringResource(R.string.guide_bouquet_button_label),
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(3.dp))
            Text(
                text = selectedBouquet,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

@Composable
private fun ChannelCard(
    assetBaseUrl: String,
    channel: GuideChannel,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    selected: Boolean,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit,
    onPlayChannel: () -> Unit
) {
    var focused by remember { mutableStateOf(false) }
    val scale by animateFloatAsState(if (focused) 1.01f else 1f, label = "channelCardScale")
    val backgroundColor by animateColorAsState(
        targetValue = when {
            focused -> colorFromRes(R.color.ide_surface_panel_soft)
            selected -> colorFromRes(R.color.ide_surface_strong)
            else -> colorFromRes(R.color.ide_surface)
        },
        label = "channelCardBackground"
    )
    val borderColor by animateColorAsState(
        targetValue = when {
            focused -> colorFromRes(R.color.ide_blue)
            selected -> colorFromRes(R.color.ide_outline_strong)
            else -> colorFromRes(R.color.ide_outline_soft)
        },
        label = "channelCardBorder"
    )

    Surface(
        modifier = modifier
            .fillMaxWidth()
            .scale(scale)
            .onFocusChanged {
                if (it.isFocused) {
                    focused = true
                    onFocus()
                } else if (!it.hasFocus) {
                    focused = false
                }
            }
            .onPreviewKeyEvent { event ->
                if (event.type == KeyEventType.KeyUp && event.key.isGuidePlayKey()) {
                    onPlayChannel()
                    true
                } else {
                    false
                }
            }
            .focusable()
            .clickable(onClick = onPlayChannel),
        shape = RoundedCornerShape(18.dp),
        color = backgroundColor,
        contentColor = MaterialTheme.colorScheme.onSurface,
        border = BorderStroke(
            width = if (focused) 2.dp else 1.dp,
            color = borderColor
        )
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(16.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                GuideChannelLogo(
                    assetBaseUrl = assetBaseUrl,
                    channel = channel
                )
                Spacer(modifier = Modifier.width(12.dp))
                Column(
                    modifier = Modifier.weight(1f)
                ) {
                    Text(
                        text = channel.displayName,
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                    channelMeta(channel)?.let { meta ->
                        Spacer(modifier = Modifier.height(3.dp))
                        Text(
                            text = meta,
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis
                        )
                    }
                }
                Spacer(modifier = Modifier.width(12.dp))
                PlayBadge()
            }
            Spacer(modifier = Modifier.height(10.dp))
            GuideProgramSummary(
                now = channel.now,
                next = channel.next,
                currentEpochSec = currentEpochSec,
                displayZoneId = displayZoneId
            )
            Spacer(modifier = Modifier.height(10.dp))
            GuideScheduleTimeline(
                schedule = channel.schedule,
                now = channel.now,
                next = channel.next,
                health = health,
                timelineWindow = timelineWindow,
                currentEpochSec = currentEpochSec,
                displayZoneId = displayZoneId
            )
        }
    }
}

@Composable
private fun GuideSelectionPanel(
    channel: GuideChannel,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    modifier: Modifier = Modifier
) {
    val primaryProgram = channelPrimaryProgram(channel, currentEpochSec) ?: return
    val description = primaryProgram.description
        ?.let(::normalizeGuideDescription)
        ?: return

    Surface(
        modifier = modifier,
        shape = RoundedCornerShape(18.dp),
        color = colorFromRes(R.color.ide_surface_panel),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        Column(
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 14.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp)
        ) {
            Text(
                text = channel.displayName,
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            Text(
                text = primaryProgram.title,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            Text(
                text = "${primaryProgram.displayStartTime(displayZoneId)}-${primaryProgram.displayEndTime(displayZoneId)}",
                style = MaterialTheme.typography.labelSmall,
                color = colorFromRes(R.color.ide_live)
            )
            Text(
                text = description,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

private fun Key.isGuidePlayKey(): Boolean = this == Key.DirectionCenter || this == Key.Enter || this == Key.NumPadEnter

@Composable
private fun PlayBadge() {
    Surface(
        shape = RoundedCornerShape(14.dp),
        color = colorFromRes(R.color.ide_blue),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_blue)),
        contentColor = colorFromRes(R.color.ide_text_primary)
    ) {
        Text(
            text = stringResource(R.string.guide_play),
            modifier = Modifier.padding(horizontal = 12.dp, vertical = 7.dp),
            style = MaterialTheme.typography.labelMedium,
            color = colorFromRes(R.color.ide_text_primary)
        )
    }
}

@Composable
private fun GuideChannelLogo(
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
        modifier = Modifier.size(58.dp),
        shape = RoundedCornerShape(14.dp),
        color = colorFromRes(R.color.ide_surface_panel),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft)),
        contentColor = MaterialTheme.colorScheme.onSurface
    ) {
        if (bitmap != null) {
            Image(
                bitmap = bitmap!!.asImageBitmap(),
                contentDescription = channel.displayName,
                contentScale = ContentScale.Fit,
                modifier = Modifier
                    .fillMaxSize()
                    .padding(6.dp)
            )
        } else {
            Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.Center
            ) {
                Text(
                    text = channelLogoFallback(channel),
                    style = MaterialTheme.typography.labelLarge,
                    fontWeight = FontWeight.SemiBold,
                    color = colorFromRes(R.color.ide_live)
                )
            }
        }
    }
}

@Composable
private fun GuideProgramSummary(
    now: GuideProgram?,
    next: GuideProgram?,
    currentEpochSec: Long,
    displayZoneId: ZoneId
) {
    val liveProgram = now?.takeIf { currentEpochSec < it.endEpochSec }
    if (liveProgram == null && next == null) {
        return
    }

    Column(
        modifier = Modifier.fillMaxWidth(),
        verticalArrangement = Arrangement.spacedBy(8.dp)
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            liveProgram?.let { program ->
                Surface(
                    shape = RoundedCornerShape(12.dp),
                    color = colorFromRes(R.color.ide_live).copy(alpha = 0.14f),
                    border = BorderStroke(1.dp, colorFromRes(R.color.ide_live).copy(alpha = 0.35f))
                ) {
                    Text(
                        text = stringResource(R.string.guide_now),
                        modifier = Modifier.padding(horizontal = 10.dp, vertical = 6.dp),
                        style = MaterialTheme.typography.labelMedium,
                        color = colorFromRes(R.color.ide_live)
                    )
                }
                Column(
                    modifier = Modifier.weight(1f)
                ) {
                    Text(
                        text = liveProgram.title,
                        style = MaterialTheme.typography.bodyMedium,
                        fontWeight = FontWeight.SemiBold,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                    Text(
                        text = "${liveProgram.displayStartTime(displayZoneId)}-${liveProgram.displayEndTime(displayZoneId)}",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }
            next?.let { program ->
                Column(
                    modifier = Modifier.weight(1f)
                ) {
                    Text(
                        text = stringResource(R.string.guide_next),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Text(
                        text = program.title,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                }
            }
        }

    }
}

@Composable
private fun GuideScheduleTimeline(
    schedule: List<GuideProgram>,
    now: GuideProgram?,
    next: GuideProgram?,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    currentEpochSec: Long,
    displayZoneId: ZoneId
) {
    val visiblePrograms = remember(schedule, now, next, timelineWindow) {
        buildGuideTimelinePrograms(
            schedule = schedule,
            fallbackPrograms = listOfNotNull(now, next).distinctBy { it.startEpochSec to it.title },
            timelineWindow = timelineWindow
        )
    }

    if (visiblePrograms.isEmpty()) {
        Surface(
            modifier = Modifier
                .fillMaxWidth()
                .height(64.dp),
            shape = RoundedCornerShape(16.dp),
            color = colorFromRes(R.color.ide_surface_panel),
            border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft)),
            contentColor = MaterialTheme.colorScheme.onSurface
        ) {
            Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.CenterStart
            ) {
                Text(
                    text = when {
                        health?.receiverHealthy == false -> stringResource(R.string.guide_no_program_receiver)
                        health?.epgHealthy == false -> stringResource(R.string.guide_no_program_syncing)
                        else -> stringResource(R.string.guide_no_program)
                    },
                    modifier = Modifier.padding(horizontal = 16.dp),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
        return
    }

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp)
    ) {
        visiblePrograms.forEach { program ->
            GuideScheduleSegment(
                program = program,
                weight = if (timelineWindow != null) {
                    program.visibleDurationSeconds(timelineWindow).coerceAtLeast(1L).toFloat()
                } else {
                    programWeight(program)
                },
                active = currentEpochSec >= program.startEpochSec && currentEpochSec < program.endEpochSec,
                displayZoneId = displayZoneId
            )
        }
    }
}

private fun buildGuideTimelinePrograms(
    schedule: List<GuideProgram>,
    fallbackPrograms: List<GuideProgram>,
    timelineWindow: GuideTimelineWindow?
): List<GuideProgram> {
    val source = if (schedule.isNotEmpty()) schedule else fallbackPrograms
    return source
        .sortedBy(GuideProgram::startEpochSec)
        .filter { program -> timelineWindow == null || program.overlaps(timelineWindow) }
        .take(4)
}

private fun channelPrimaryProgram(
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

private fun normalizeGuideDescription(raw: String): String? =
    raw
        .replace("\\n", " ")
        .replace(Regex("\\s+"), " ")
        .trim()
        .takeIf { it.isNotEmpty() }

@Composable
private fun RowScope.GuideScheduleSegment(
    program: GuideProgram,
    weight: Float,
    active: Boolean,
    displayZoneId: ZoneId
) {
    val backgroundColor = if (active) {
        colorFromRes(R.color.ide_surface_panel)
    } else {
        colorFromRes(R.color.ide_surface_panel_soft)
    }
    Box(
        modifier = Modifier
            .weight(weight)
            .heightIn(min = 70.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(backgroundColor)
    ) {
        if (active) {
            Box(
                modifier = Modifier
                    .fillMaxHeight()
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(16.dp))
                    .background(
                        Brush.horizontalGradient(
                            colors = listOf(
                                colorFromRes(R.color.ide_blue).copy(alpha = 0.22f),
                                colorFromRes(R.color.ide_live).copy(alpha = 0.18f)
                            )
                        )
                    )
            )
        }
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 12.dp, vertical = 9.dp),
            verticalArrangement = Arrangement.SpaceBetween
        ) {
            Text(
                text = if (active) stringResource(R.string.guide_now) else program.displayStartTime(displayZoneId),
                style = MaterialTheme.typography.labelSmall,
                color = if (active) {
                    colorFromRes(R.color.ide_live)
                } else {
                    colorFromRes(R.color.ide_text_secondary)
                }
            )
            Text(
                text = program.title,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.SemiBold,
                color = colorFromRes(R.color.ide_text_primary),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis
            )
            Text(
                text = "${program.displayStartTime(displayZoneId)}-${program.displayEndTime(displayZoneId)}",
                style = MaterialTheme.typography.labelSmall,
                color = colorFromRes(R.color.ide_text_primary),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

private fun programWeight(program: GuideProgram): Float {
    val durationSec = max(1L, program.endEpochSec - program.startEpochSec)
    return durationSec.toFloat()
}

private fun channelMeta(channel: GuideChannel): String? = buildList {
    channel.resolution?.takeIf { it.isNotBlank() }?.let(::add)
    channel.codec?.takeIf { it.isNotBlank() }?.uppercase()?.let(::add)
}.takeIf { it.isNotEmpty() }?.joinToString(" · ")

private fun channelLogoFallback(channel: GuideChannel): String {
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

private fun resolveGuideLogoUrl(baseUrl: String, logoUrl: String?): String? {
    val normalized = logoUrl?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    if (normalized.startsWith("http://") || normalized.startsWith("https://")) {
        return normalized
    }
    val base = baseUrl.toHttpUrlOrNull() ?: return null
    return base.resolve(normalized)?.toString()
}

private suspend fun loadGuideBitmap(url: String): Bitmap? = withContext(Dispatchers.IO) {
    runCatching {
        val connection = URL(url).openConnection() as HttpURLConnection
        val cookies = CookieManager.getInstance().getCookie(url)
        if (!cookies.isNullOrBlank()) {
            connection.setRequestProperty("Cookie", cookies)
        }
        connection.connectTimeout = 4_000
        connection.readTimeout = 4_000
        connection.inputStream.use(BitmapFactory::decodeStream)
    }.getOrNull()
}

private fun millisUntilNextProgressTick(): Long {
    val now = System.currentTimeMillis()
    val remainder = now % 30_000L
    return if (remainder == 0L) 30_000L else 30_000L - remainder
}

@Composable
private fun colorFromRes(resId: Int): Color =
    Color(ContextCompat.getColor(LocalContext.current, resId))
