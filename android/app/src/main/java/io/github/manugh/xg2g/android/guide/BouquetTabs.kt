package io.github.manugh.xg2g.android.guide

import android.view.KeyEvent
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.input.key.Key
import androidx.compose.ui.input.key.KeyEventType
import androidx.compose.ui.input.key.key
import androidx.compose.ui.input.key.onPreviewKeyEvent
import androidx.compose.ui.input.key.type
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import io.github.manugh.xg2g.android.R

@Composable
internal fun BouquetTabs(
    bouquets: List<GuideBouquet>,
    selectedBouquet: String,
    onSelectBouquet: (String) -> Unit,
    onFocusedPane: () -> Unit,
    modifier: Modifier = Modifier,
    requesters: Map<String, FocusRequester> = emptyMap()
) {
    val bouquetKeys = remember(bouquets) { bouquets.map(GuideBouquet::name) }
    val listState = rememberLazyListState()

    LaunchedEffect(selectedBouquet, bouquetKeys) {
        val index = bouquetKeys.indexOf(selectedBouquet)
        if (index >= 0) {
            listState.animateScrollToItem(index)
        }
    }

    LazyRow(
        state = listState,
        modifier = modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        contentPadding = PaddingValues(horizontal = 4.dp, vertical = 2.dp)
    ) {
        items(bouquets, key = { it.name }) { bouquet ->
            val isSelected = bouquet.name == selectedBouquet
            val requester = requesters[bouquet.name]

            OutlinedButton(
                onClick = { onSelectBouquet(bouquet.name) },
                modifier = Modifier
                    .let { mod -> if (requester != null) mod.focusRequester(requester) else mod }
                    .onFocusChanged {
                        if (it.isFocused) {
                            onFocusedPane()
                        }
                    },
                shape = RoundedCornerShape(999.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    containerColor = if (isSelected) {
                        colorResource(R.color.color_action)
                    } else {
                        colorResource(R.color.color_surface_panel_soft)
                    },
                    contentColor = colorResource(R.color.color_text_primary)
                ),
                border = BorderStroke(
                    1.dp,
                    if (isSelected) colorResource(R.color.color_action) else colorResource(R.color.color_border_subtle)
                ),
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp)
            ) {
                Text(
                    text = bouquet.name,
                    style = MaterialTheme.typography.labelLarge,
                    fontWeight = if (isSelected) FontWeight.Bold else FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )
            }
        }
    }
}

@Composable
internal fun BouquetSelectorButton(
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
        shape = MaterialTheme.shapes.large,
        colors = ButtonDefaults.outlinedButtonColors(
            containerColor = colorResource(R.color.color_surface_panel_soft),
            contentColor = colorResource(R.color.color_text_primary)
        ),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
    ) {
        Column(
            horizontalAlignment = androidx.compose.ui.Alignment.Start
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
internal fun BouquetPickerOverlay(
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
        shape = MaterialTheme.shapes.large,
        color = colorResource(R.color.color_surface_panel),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_base)),
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
                        shape = MaterialTheme.shapes.large,
                        colors = ButtonDefaults.outlinedButtonColors(
                            containerColor = if (selected) {
                                colorResource(R.color.color_action)
                            } else {
                                colorResource(R.color.color_surface_panel_soft)
                            },
                            contentColor = colorResource(R.color.color_text_primary)
                        ),
                        border = BorderStroke(
                            1.dp,
                            if (selected) colorResource(R.color.color_action) else colorResource(R.color.color_border_subtle)
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
                                        colorResource(R.color.color_text_primary)
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
