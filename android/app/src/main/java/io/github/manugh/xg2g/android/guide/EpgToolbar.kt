package io.github.manugh.xg2g.android.guide

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.focusProperties
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import io.github.manugh.xg2g.android.R
import java.time.ZoneId

@Composable
internal fun GuideHeader(
    serverLabel: String,
    health: GuideHealthStatus?,
    timelineWindow: GuideTimelineWindow?,
    displayZoneId: ZoneId,
    isRefreshing: Boolean,
    onRefresh: () -> Unit
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(bottom = 6.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            Text(
                text = stringResource(R.string.guide_title),
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Bold,
                color = MaterialTheme.colorScheme.onBackground
            )
            GuideHealthChip(health)
            GuideWindowChip(timelineWindow, displayZoneId)
        }

        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            GuideServerChip(serverLabel)
            if (isRefreshing) {
                CircularProgressIndicator(
                    modifier = Modifier.size(18.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.primary
                )
            }
            OutlinedButton(
                onClick = onRefresh,
                modifier = Modifier.focusProperties { canFocus = false },
                shape = RoundedCornerShape(12.dp),
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 6.dp),
                colors = ButtonDefaults.outlinedButtonColors(
                    containerColor = colorResource(R.color.color_surface_panel_soft),
                    contentColor = MaterialTheme.colorScheme.onSurface
                ),
                border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
            ) {
                Text(
                    text = stringResource(R.string.guide_refresh),
                    style = MaterialTheme.typography.labelMedium
                )
            }
        }
    }
}

@Composable
internal fun GuideWindowChip(timelineWindow: GuideTimelineWindow?, displayZoneId: ZoneId) {
    if (timelineWindow == null) {
        return
    }

    Surface(
        shape = RoundedCornerShape(14.dp),
        color = colorResource(R.color.color_surface_panel_soft),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle)),
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
internal fun GuideHealthChip(health: GuideHealthStatus?) {
    if (health == null) {
        return
    }

    val (labelRes, tone) = when {
        !health.receiverHealthy -> R.string.guide_health_receiver_issue to colorResource(R.color.color_status_error)
        health.epgHealthy -> R.string.guide_health_epg_ready to colorResource(R.color.color_live)
        else -> R.string.guide_health_epg_limited to colorResource(R.color.color_live)
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
internal fun GuideServerChip(serverLabel: String) {
    Surface(
        shape = RoundedCornerShape(14.dp),
        color = colorResource(R.color.color_surface_panel_soft),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle)),
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
