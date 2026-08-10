package io.github.manugh.xg2g.android.ui.navigation

import android.view.KeyEvent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
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
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.List
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Timer
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.scale
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.key.onKeyEvent
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.TvNavigationDestination

@Composable
internal fun BroadcastRail(
    currentDestination: TvNavigationDestination,
    onNavigate: (TvNavigationDestination) -> Unit,
    onFocusChanged: (Boolean) -> Unit,
    modifier: Modifier = Modifier
) {
    var isRailFocused by remember { mutableStateOf(false) }
    val animatedWidth by animateDpAsState(
        targetValue = if (isRailFocused) 240.dp else 72.dp,
        label = "railWidth"
    )

    Box(
        modifier = modifier
            .width(animatedWidth)
            .fillMaxHeight()
            .clip(RoundedCornerShape(topEnd = 16.dp, bottomEnd = 16.dp))
            .background(
                Brush.horizontalGradient(
                    colors = listOf(
                        colorResource(R.color.color_surface_shell_strong),
                        colorResource(R.color.color_surface_panel)
                    )
                )
            )
            .border(
                width = 1.dp,
                brush = Brush.horizontalGradient(
                    colors = listOf(
                        colorResource(R.color.color_border_base),
                        colorResource(R.color.color_border_subtle)
                    )
                ),
                shape = RoundedCornerShape(topEnd = 16.dp, bottomEnd = 16.dp)
            )
            .onFocusChanged { state ->
                isRailFocused = state.hasFocus
                onFocusChanged(state.hasFocus)
            }
    ) {
        Column(
            modifier = Modifier
                .fillMaxHeight()
                .padding(vertical = 20.dp, horizontal = 10.dp),
            horizontalAlignment = Alignment.Start,
            verticalArrangement = Arrangement.Top
        ) {
            // Header / Console Brand Mark
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(48.dp)
                    .padding(horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.Start
            ) {
                // Live Status Indicator Dot
                Box(
                    modifier = Modifier
                        .size(10.dp)
                        .background(colorResource(R.color.color_action), CircleShape)
                )
                
                AnimatedVisibility(
                    visible = isRailFocused,
                    enter = fadeIn(),
                    exit = fadeOut()
                ) {
                    Text(
                        text = "CONSOLE",
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold,
                        color = colorResource(R.color.color_text_primary),
                        modifier = Modifier.padding(start = 14.dp)
                    )
                }
            }

            Spacer(modifier = Modifier.height(24.dp))

            // Navigation Items
            RailItem(
                icon = Icons.Default.Home,
                label = "Dashboard",
                isActive = currentDestination == TvNavigationDestination.Home,
                isExpanded = isRailFocused,
                onClick = { onNavigate(TvNavigationDestination.Home) }
            )
            
            Spacer(modifier = Modifier.height(10.dp))

            RailItem(
                icon = Icons.AutoMirrored.Filled.List,
                label = "TV Guide",
                isActive = currentDestination == TvNavigationDestination.Guide,
                isExpanded = isRailFocused,
                onClick = { onNavigate(TvNavigationDestination.Guide) }
            )

            Spacer(modifier = Modifier.height(10.dp))

            RailItem(
                icon = Icons.Default.PlayArrow,
                label = "Recordings",
                isActive = currentDestination == TvNavigationDestination.Recordings,
                isExpanded = isRailFocused,
                onClick = { onNavigate(TvNavigationDestination.Recordings) }
            )

            Spacer(modifier = Modifier.height(10.dp))

            RailItem(
                icon = Icons.Default.Timer,
                label = "Timers",
                isActive = currentDestination == TvNavigationDestination.Timers,
                isExpanded = isRailFocused,
                onClick = { onNavigate(TvNavigationDestination.Timers) }
            )

            Spacer(modifier = Modifier.height(10.dp))

            RailItem(
                icon = Icons.Default.Settings,
                label = "Settings",
                isActive = currentDestination == TvNavigationDestination.Settings,
                isExpanded = isRailFocused,
                onClick = { onNavigate(TvNavigationDestination.Settings) }
            )
        }
    }
}

@Composable
private fun RailItem(
    icon: ImageVector,
    label: String,
    isActive: Boolean,
    isExpanded: Boolean,
    onClick: () -> Unit
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    val scale by animateFloatAsState(
        targetValue = if (isFocused) 1.05f else 1.0f,
        label = "itemScale"
    )

    val actionColor = colorResource(R.color.color_action)
    val textPrimary = colorResource(R.color.color_text_primary)
    val textSecondary = colorResource(R.color.color_text_secondary)
    val borderBase = colorResource(R.color.color_border_base)
    val borderSubtle = colorResource(R.color.color_border_subtle)

    val itemBackgroundColor = when {
        isFocused -> actionColor.copy(alpha = 0.25f)
        isActive -> colorResource(R.color.color_bg_elevated)
        else -> Color.Transparent
    }

    val itemBorderColor = when {
        isFocused -> actionColor
        isActive -> borderBase
        else -> borderSubtle
    }

    val itemContentColor = when {
        isFocused -> Color.White
        isActive -> actionColor
        else -> textSecondary
    }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(52.dp)
            .scale(scale)
            .clip(RoundedCornerShape(12.dp))
            .background(itemBackgroundColor)
            .border(
                width = if (isFocused) 2.dp else 1.dp,
                color = itemBorderColor,
                shape = RoundedCornerShape(12.dp)
            )
            .focusable(interactionSource = interactionSource)
            .onKeyEvent { event ->
                if (event.nativeKeyEvent.action == KeyEvent.ACTION_DOWN) {
                    when (event.nativeKeyEvent.keyCode) {
                        KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER -> {
                            onClick()
                            true
                        }
                        else -> false
                    }
                } else false
            }
            .padding(horizontal = 12.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        // Active Destination Bar Indicator
        Box(
            modifier = Modifier
                .width(4.dp)
                .height(24.dp)
                .clip(RoundedCornerShape(2.dp))
                .background(
                    if (isActive) actionColor else Color.Transparent
                )
        )

        Spacer(modifier = Modifier.width(10.dp))

        Icon(
            imageVector = icon,
            contentDescription = label,
            tint = itemContentColor,
            modifier = Modifier.size(24.dp)
        )

        AnimatedVisibility(
            visible = isExpanded,
            enter = fadeIn(),
            exit = fadeOut()
        ) {
            Text(
                text = label,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = if (isFocused || isActive) FontWeight.Bold else FontWeight.Medium,
                color = if (isFocused) textPrimary else itemContentColor,
                modifier = Modifier.padding(start = 14.dp)
            )
        }
    }
}
