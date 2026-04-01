package io.github.manugh.xg2g.android.guide

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.appcompat.app.AppCompatActivity
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.delay
import kotlin.math.max

class GuideActivity : AppCompatActivity() {
    private lateinit var baseUrl: String
    private var authToken: String? = null
    private val playbackBridge by lazy(LazyThreadSafetyMode.NONE) { NativePlaybackBridge(this) }
    private val viewModel: GuideViewModel by viewModels {
        GuideViewModel.Factory(
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
    onSelectBouquet: (String) -> Unit,
    onSelectChannel: (String) -> Unit,
    onRefresh: () -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onExit: () -> Unit
) {
    val currentEpochSec by produceState(initialValue = Instant.now().epochSecond) {
        while (true) {
            value = Instant.now().epochSecond
            delay(millisUntilNextProgressTick())
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
            .padding(horizontal = 28.dp, vertical = 24.dp)
    ) {
        Column(
            modifier = Modifier.fillMaxSize()
        ) {
            GuideHeader(
                serverLabel = state.serverLabel,
                isRefreshing = state is GuideScreenState.Ready && state.isRefreshing,
                onRefresh = onRefresh
            )
            Spacer(modifier = Modifier.height(24.dp))

            when (state) {
                is GuideScreenState.Loading -> GuideLoading(state.serverLabel)
                is GuideScreenState.Error -> GuideError(state)
                is GuideScreenState.Empty -> GuideContentLayout(
                    bouquets = state.bouquets,
                    selectedBouquet = state.selectedBouquet,
                    channels = emptyList(),
                    selectedChannelRef = null,
                    currentEpochSec = currentEpochSec,
                    onSelectBouquet = onSelectBouquet,
                    onSelectChannel = onSelectChannel,
                    onPlayChannel = onPlayChannel,
                    onExit = onExit
                )
                is GuideScreenState.Ready -> GuideContentLayout(
                    bouquets = state.bouquets,
                    selectedBouquet = state.selectedBouquet,
                    channels = state.channels,
                    selectedChannelRef = state.selectedChannelRef,
                    currentEpochSec = currentEpochSec,
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
private fun GuideHeader(
    serverLabel: String,
    isRefreshing: Boolean,
    onRefresh: () -> Unit
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.Top
    ) {
        Column(
            modifier = Modifier.weight(1f)
        ) {
            Text(
                text = stringResource(R.string.guide_kicker),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.secondary
            )
            Spacer(modifier = Modifier.height(6.dp))
            Text(
                text = stringResource(R.string.guide_title),
                style = MaterialTheme.typography.headlineMedium,
                fontWeight = FontWeight.SemiBold
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.guide_support),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(12.dp))
            Text(
                text = serverLabel,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }

        Spacer(modifier = Modifier.width(20.dp))

        Column(
            horizontalAlignment = Alignment.End
        ) {
            OutlinedButton(
                onClick = onRefresh,
                modifier = Modifier.focusProperties { canFocus = false },
                shape = RoundedCornerShape(20.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    containerColor = colorFromRes(R.color.ide_surface_panel_soft),
                    contentColor = MaterialTheme.colorScheme.onSurface
                ),
                border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft))
            ) {
                Text(stringResource(R.string.guide_refresh))
            }
            if (isRefreshing) {
                Spacer(modifier = Modifier.height(12.dp))
                LinearProgressIndicator(
                    modifier = Modifier.width(180.dp),
                    color = MaterialTheme.colorScheme.primary,
                    trackColor = MaterialTheme.colorScheme.surfaceVariant
                )
            }
        }
    }
}

@Composable
private fun GuideLoading(serverLabel: String) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(28.dp),
        color = colorFromRes(R.color.ide_surface_strong),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline))
    ) {
        Column(
            modifier = Modifier.padding(28.dp)
        ) {
            Text(
                text = stringResource(R.string.guide_loading),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.SemiBold
            )
            Spacer(modifier = Modifier.height(10.dp))
            Text(
                text = stringResource(R.string.guide_loading_detail, serverLabel),
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(18.dp))
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
        shape = RoundedCornerShape(28.dp),
        color = colorFromRes(R.color.ide_surface_strong),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline))
    ) {
        Column(
            modifier = Modifier.padding(28.dp)
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
                    stringResource(R.string.guide_auth_detail)
                } else {
                    state.detail.ifBlank { stringResource(R.string.guide_generic_detail) }
                },
                style = MaterialTheme.typography.bodyLarge,
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
    selectedChannelRef: String?,
    currentEpochSec: Long,
    onSelectBouquet: (String) -> Unit,
    onSelectChannel: (String) -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onExit: () -> Unit
) {
    val bouquetKeys = remember(bouquets) { bouquets.map(GuideBouquet::name) }
    val channelKeys = remember(channels) { channels.map(GuideChannel::serviceRef) }
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

    LaunchedEffect(selectedBouquet, bouquetKeys) {
        val index = bouquetKeys.indexOf(selectedBouquet)
        if (index >= 0) {
            bouquetListState.scrollToItem(index)
        }
    }

    LaunchedEffect(selectedChannelKey, channelKeys) {
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

    BackHandler {
        if (focusedPane == GuideFocusedPane.CHANNELS && selectedBouquetRequester != null) {
            selectedBouquetRequester.requestFocus()
            focusedPane = GuideFocusedPane.BOUQUETS
        } else {
            onExit()
        }
    }

    Row(
        modifier = Modifier.fillMaxSize()
    ) {
        BouquetRail(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            listState = bouquetListState,
            requesters = bouquetRequesters,
            channelFocusRequester = selectedChannelRequester,
            onSelectBouquet = onSelectBouquet,
            onFocusedPane = { focusedPane = GuideFocusedPane.BOUQUETS },
            modifier = Modifier
                .width(260.dp)
                .fillMaxHeight()
        )
        Spacer(modifier = Modifier.width(20.dp))
        ChannelPane(
            channels = channels,
            selectedChannelRef = selectedChannelRef,
            currentEpochSec = currentEpochSec,
            listState = channelListState,
            requesters = channelRequesters,
            bouquetFocusRequester = selectedBouquetRequester,
            onSelectChannel = onSelectChannel,
            onPlayChannel = onPlayChannel,
            onFocusedPane = { focusedPane = GuideFocusedPane.CHANNELS },
            modifier = Modifier.weight(1f)
        )
    }
}

@Composable
private fun BouquetRail(
    bouquets: List<GuideBouquet>,
    selectedBouquet: String,
    listState: androidx.compose.foundation.lazy.LazyListState,
    requesters: Map<String, FocusRequester>,
    channelFocusRequester: FocusRequester?,
    onSelectBouquet: (String) -> Unit,
    onFocusedPane: () -> Unit,
    modifier: Modifier = Modifier
) {
    Surface(
        modifier = modifier,
        shape = RoundedCornerShape(28.dp),
        color = colorFromRes(R.color.ide_surface_panel),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline))
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(18.dp)
        ) {
            Text(
                text = stringResource(R.string.guide_bouquets),
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold
            )
            Spacer(modifier = Modifier.height(14.dp))
            LazyColumn(
                state = listState,
                verticalArrangement = Arrangement.spacedBy(10.dp)
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
                                right = channelFocusRequester ?: FocusRequester.Default
                            }
                            .onFocusChanged {
                                if (it.isFocused) {
                                    onFocusedPane()
                                }
                            },
                        shape = RoundedCornerShape(22.dp),
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
    channels: List<GuideChannel>,
    selectedChannelRef: String?,
    currentEpochSec: Long,
    listState: androidx.compose.foundation.lazy.LazyListState,
    requesters: Map<String, FocusRequester>,
    bouquetFocusRequester: FocusRequester?,
    onSelectChannel: (String) -> Unit,
    onPlayChannel: (GuideChannel) -> Unit,
    onFocusedPane: () -> Unit,
    modifier: Modifier = Modifier
) {
    Surface(
        modifier = modifier.fillMaxHeight(),
        shape = RoundedCornerShape(28.dp),
        color = colorFromRes(R.color.ide_surface_strong),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline))
    ) {
        if (channels.isEmpty()) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(28.dp),
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
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
            return@Surface
        }

        LazyColumn(
            state = listState,
            modifier = Modifier
                .fillMaxSize()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            itemsIndexed(channels, key = { _, channel -> channel.serviceRef }) { _, channel ->
                ChannelCard(
                    channel = channel,
                    currentEpochSec = currentEpochSec,
                    selected = channel.serviceRef == selectedChannelRef,
                    modifier = Modifier
                        .focusRequester(requesters.getValue(channel.serviceRef))
                        .focusProperties {
                            left = bouquetFocusRequester ?: FocusRequester.Default
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

@Composable
private fun ChannelCard(
    channel: GuideChannel,
    currentEpochSec: Long,
    selected: Boolean,
    modifier: Modifier = Modifier,
    onFocus: () -> Unit,
    onPlayChannel: () -> Unit
) {
    var focused by remember { mutableStateOf(false) }
    val scale by animateFloatAsState(if (focused) 1.015f else 1f, label = "channelCardScale")
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
        shape = RoundedCornerShape(24.dp),
        color = backgroundColor,
        border = BorderStroke(
            width = if (focused) 2.dp else 1.dp,
            color = borderColor
        )
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(20.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = channel.displayName,
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f)
                )
                Spacer(modifier = Modifier.width(16.dp))
                PlayBadge()
            }
            Spacer(modifier = Modifier.height(14.dp))
            NowNextTimeline(
                now = channel.now,
                next = channel.next,
                currentEpochSec = currentEpochSec
            )
        }
    }
}

private fun Key.isGuidePlayKey(): Boolean = this == Key.DirectionCenter || this == Key.Enter || this == Key.NumPadEnter

@Composable
private fun PlayBadge() {
    Surface(
        shape = RoundedCornerShape(16.dp),
        color = colorFromRes(R.color.ide_blue),
        border = BorderStroke(1.dp, colorFromRes(R.color.ide_blue))
    ) {
        Text(
            text = stringResource(R.string.guide_play),
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 8.dp),
            style = MaterialTheme.typography.labelLarge,
            color = colorFromRes(R.color.ide_text_primary)
        )
    }
}

@Composable
private fun NowNextTimeline(
    now: GuideProgram?,
    next: GuideProgram?,
    currentEpochSec: Long
) {
    if (now == null && next == null) {
        Surface(
            modifier = Modifier
                .fillMaxWidth()
                .height(82.dp),
            shape = RoundedCornerShape(20.dp),
            color = colorFromRes(R.color.ide_surface_panel),
            border = BorderStroke(1.dp, colorFromRes(R.color.ide_outline_soft))
        ) {
            Box(
                modifier = Modifier.fillMaxSize(),
                contentAlignment = Alignment.CenterStart
            ) {
                Text(
                    text = stringResource(R.string.guide_no_program),
                    modifier = Modifier.padding(horizontal = 16.dp),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
        return
    }

    val nowWeight = now?.let { programWeight(it) } ?: 0f
    val nextWeight = next?.let { programWeight(it) } ?: 0f

    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(10.dp)
    ) {
        if (now != null) {
            TimelineSegment(
                label = stringResource(R.string.guide_now),
                program = now,
                weight = if (next != null) nowWeight else 1f,
                progress = programProgress(now, currentEpochSec),
                backgroundColor = colorFromRes(R.color.ide_surface_panel),
                fillBrush = Brush.horizontalGradient(
                    colors = listOf(
                        colorFromRes(R.color.ide_blue),
                        colorFromRes(R.color.ide_live)
                    )
                )
            )
        }
        if (next != null) {
            TimelineSegment(
                label = stringResource(R.string.guide_next),
                program = next,
                weight = if (now != null) nextWeight else 1f,
                progress = null,
                backgroundColor = colorFromRes(R.color.ide_surface_panel_soft),
                fillBrush = null
            )
        }
    }
}

@Composable
private fun RowScope.TimelineSegment(
    label: String,
    program: GuideProgram,
    weight: Float,
    progress: Float?,
    backgroundColor: Color,
    fillBrush: Brush?
) {
    Box(
        modifier = Modifier
            .weight(weight)
            .heightIn(min = 82.dp)
            .clip(RoundedCornerShape(20.dp))
            .background(backgroundColor)
    ) {
        if (progress != null && fillBrush != null) {
            Box(
                modifier = Modifier
                    .fillMaxHeight()
                    .fillMaxWidth(progress.coerceIn(0f, 1f))
                    .clip(RoundedCornerShape(20.dp))
                    .background(fillBrush)
            )
        }
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 14.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.SpaceBetween
        ) {
            Text(
                text = label,
                style = MaterialTheme.typography.labelMedium,
                color = colorFromRes(R.color.ide_text_secondary)
            )
            Text(
                text = program.title,
                style = MaterialTheme.typography.bodyMedium,
                fontWeight = FontWeight.SemiBold,
                color = colorFromRes(R.color.ide_text_primary),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            Text(
                text = "${formatTime(program.startEpochSec)}-${formatTime(program.endEpochSec)}",
                style = MaterialTheme.typography.labelMedium,
                color = colorFromRes(R.color.ide_text_primary),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

private fun formatTime(epochSec: Long): String =
    TIME_FORMATTER.format(Instant.ofEpochSecond(epochSec))

private fun programWeight(program: GuideProgram): Float {
    val durationSec = max(1L, program.endEpochSec - program.startEpochSec)
    return durationSec.toFloat()
}

private fun programProgress(program: GuideProgram, currentEpochSec: Long): Float {
    val duration = max(1L, program.endEpochSec - program.startEpochSec).toFloat()
    val elapsed = (currentEpochSec - program.startEpochSec).toFloat()
    return (elapsed / duration).coerceIn(0f, 1f)
}

private fun millisUntilNextProgressTick(): Long {
    val now = System.currentTimeMillis()
    val remainder = now % 30_000L
    return if (remainder == 0L) 30_000L else 30_000L - remainder
}

@Composable
private fun colorFromRes(resId: Int): Color =
    Color(ContextCompat.getColor(LocalContext.current, resId))

private val TIME_FORMATTER: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
    .withZone(ZoneId.systemDefault())
