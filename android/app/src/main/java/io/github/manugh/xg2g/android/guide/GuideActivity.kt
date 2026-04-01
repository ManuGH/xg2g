package io.github.manugh.xg2g.android.guide

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.appcompat.app.AppCompatActivity
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
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
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
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
                    onRefresh = viewModel::refresh,
                    onPlayChannel = ::playChannel
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
    onRefresh: () -> Unit,
    onPlayChannel: (GuideChannel) -> Unit
) {
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
                    onSelectBouquet = onSelectBouquet,
                    onPlayChannel = onPlayChannel
                )
                is GuideScreenState.Ready -> GuideContentLayout(
                    bouquets = state.bouquets,
                    selectedBouquet = state.selectedBouquet,
                    channels = state.channels,
                    onSelectBouquet = onSelectBouquet,
                    onPlayChannel = onPlayChannel
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
    onSelectBouquet: (String) -> Unit,
    onPlayChannel: (GuideChannel) -> Unit
) {
    Row(
        modifier = Modifier.fillMaxSize()
    ) {
        BouquetRail(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            onSelectBouquet = onSelectBouquet,
            modifier = Modifier
                .width(260.dp)
                .fillMaxHeight()
        )
        Spacer(modifier = Modifier.width(20.dp))
        ChannelPane(
            channels = channels,
            modifier = Modifier.weight(1f),
            onPlayChannel = onPlayChannel
        )
    }
}

@Composable
private fun BouquetRail(
    bouquets: List<GuideBouquet>,
    selectedBouquet: String,
    onSelectBouquet: (String) -> Unit,
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
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                items(bouquets, key = { it.name }) { bouquet ->
                    val selected = bouquet.name == selectedBouquet
                    OutlinedButton(
                        onClick = { onSelectBouquet(bouquet.name) },
                        modifier = Modifier.fillMaxWidth(),
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
    modifier: Modifier = Modifier,
    onPlayChannel: (GuideChannel) -> Unit
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

        val firstChannelRequester = remember { FocusRequester() }
        LaunchedEffect(channels.firstOrNull()?.serviceRef) {
            if (channels.isNotEmpty()) {
                firstChannelRequester.requestFocus()
            }
        }

        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(18.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            itemsIndexed(channels, key = { _, channel -> channel.serviceRef }) { index, channel ->
                ChannelCard(
                    channel = channel,
                    modifier = if (index == 0) {
                        Modifier.focusRequester(firstChannelRequester)
                    } else {
                        Modifier
                    },
                    onPlayChannel = onPlayChannel
                )
            }
        }
    }
}

@Composable
private fun ChannelCard(
    channel: GuideChannel,
    modifier: Modifier = Modifier,
    onPlayChannel: (GuideChannel) -> Unit
) {
    var focused by remember { mutableStateOf(false) }
    val scale by animateFloatAsState(if (focused) 1.015f else 1f, label = "channelCardScale")

    Surface(
        modifier = modifier
            .fillMaxWidth()
            .scale(scale)
            .onFocusChanged { focused = it.isFocused }
            .focusable()
            .clickable { onPlayChannel(channel) },
        shape = RoundedCornerShape(24.dp),
        color = if (focused) {
            colorFromRes(R.color.ide_surface_panel_soft)
        } else {
            colorFromRes(R.color.ide_surface)
        },
        border = BorderStroke(
            width = if (focused) 2.dp else 1.dp,
            color = if (focused) {
                colorFromRes(R.color.ide_blue)
            } else {
                colorFromRes(R.color.ide_outline_soft)
            }
        )
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(20.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
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
                Spacer(modifier = Modifier.height(6.dp))
                ProgramLine(
                    label = stringResource(R.string.guide_now),
                    program = channel.now
                )
                Spacer(modifier = Modifier.height(6.dp))
                ProgramLine(
                    label = stringResource(R.string.guide_next),
                    program = channel.next
                )
            }
            Spacer(modifier = Modifier.width(18.dp))
            Button(
                onClick = { onPlayChannel(channel) },
                shape = RoundedCornerShape(20.dp),
                colors = ButtonDefaults.buttonColors(
                    containerColor = colorFromRes(R.color.ide_blue),
                    contentColor = colorFromRes(R.color.ide_text_primary)
                )
            ) {
                Text(stringResource(R.string.guide_play))
            }
        }
    }
}

@Composable
private fun ProgramLine(
    label: String,
    program: GuideProgram?
) {
    val text = if (program == null) {
        stringResource(R.string.guide_no_program)
    } else {
        "${formatTime(program.startEpochSec)}-${formatTime(program.endEpochSec)}  ${program.title}"
    }
    Text(
        text = "$label  $text",
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis
    )
}

private fun formatTime(epochSec: Long): String =
    TIME_FORMATTER.format(Instant.ofEpochSecond(epochSec))

@Composable
private fun colorFromRes(resId: Int): Color =
    Color(ContextCompat.getColor(LocalContext.current, resId))

private val TIME_FORMATTER: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
    .withZone(ZoneId.systemDefault())
