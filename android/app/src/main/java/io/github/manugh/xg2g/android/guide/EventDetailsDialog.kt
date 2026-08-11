package io.github.manugh.xg2g.android.guide

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import io.github.manugh.xg2g.android.R
import java.time.ZoneId

@Composable
internal fun EventDetailsDialog(
    channel: GuideChannel,
    program: GuideProgram,
    currentEpochSec: Long,
    displayZoneId: ZoneId,
    onPlayChannel: () -> Unit,
    onDismiss: () -> Unit
) {
    val description = program.description?.let(::normalizeGuideDescription)
    val isLive = currentEpochSec in program.startEpochSec until program.endEpochSec
    val remainingMinutes = if (isLive) {
        ((program.endEpochSec - currentEpochSec) / 60L).coerceAtLeast(1L)
    } else null

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = RoundedCornerShape(20.dp),
            color = colorResource(R.color.color_surface_panel),
            border = BorderStroke(1.dp, colorResource(R.color.color_border_base)),
            contentColor = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.fillMaxWidth(0.9f)
        ) {
            Column(
                modifier = Modifier.padding(20.dp),
                verticalArrangement = Arrangement.spacedBy(14.dp)
            ) {
                // Header: Channel & Time
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = Color(0x3300E5FF),
                        border = BorderStroke(1.dp, Color(0x6600E5FF))
                    ) {
                        Text(
                            text = channel.displayName,
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
                            style = MaterialTheme.typography.labelMedium,
                            fontWeight = FontWeight.Bold,
                            color = Color(0xFF00E5FF)
                        )
                    }

                    Surface(
                        shape = RoundedCornerShape(8.dp),
                        color = colorResource(R.color.color_surface_panel_soft),
                        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
                    ) {
                        Text(
                            text = "${program.displayStartTime(displayZoneId)} - ${program.displayEndTime(displayZoneId)}",
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 4.dp),
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                }

                // Title
                Text(
                    text = program.title,
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                    color = Color.White
                )

                // Live status & remaining time
                if (isLive) {
                    Row(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Surface(
                            shape = RoundedCornerShape(8.dp),
                            color = colorResource(R.color.color_live).copy(alpha = 0.2f),
                            border = BorderStroke(1.dp, colorResource(R.color.color_live).copy(alpha = 0.5f))
                        ) {
                            Text(
                                text = "● LIVE NOW",
                                modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp),
                                style = MaterialTheme.typography.labelSmall,
                                fontWeight = FontWeight.Bold,
                                color = colorResource(R.color.color_live)
                            )
                        }

                        if (remainingMinutes != null) {
                            Text(
                                text = "Noch $remainingMinutes Minuten",
                                style = MaterialTheme.typography.bodyMedium,
                                color = Color.White.copy(alpha = 0.8f)
                            )
                        }
                    }
                }

                // Description
                if (!description.isNullOrBlank()) {
                    Text(
                        text = description,
                        style = MaterialTheme.typography.bodyMedium,
                        color = Color.White.copy(alpha = 0.75f),
                        maxLines = 6,
                        overflow = TextOverflow.Ellipsis
                    )
                }

                Spacer(modifier = Modifier.height(6.dp))

                // Actions
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.End,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    OutlinedButton(
                        onClick = onDismiss,
                        shape = RoundedCornerShape(12.dp),
                        colors = ButtonDefaults.outlinedButtonColors(
                            containerColor = colorResource(R.color.color_surface_panel_soft),
                            contentColor = Color.White
                        ),
                        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
                    ) {
                        Text(stringResource(R.string.server_setup_cancel))
                    }

                    Spacer(modifier = Modifier.padding(horizontal = 6.dp))

                    OutlinedButton(
                        onClick = {
                            onDismiss()
                            onPlayChannel()
                        },
                        shape = RoundedCornerShape(12.dp),
                        colors = ButtonDefaults.outlinedButtonColors(
                            containerColor = colorResource(R.color.color_action),
                            contentColor = Color.White
                        ),
                        border = BorderStroke(1.dp, colorResource(R.color.color_action))
                    ) {
                        Text(
                            text = stringResource(R.string.guide_play),
                            fontWeight = FontWeight.Bold
                        )
                    }
                }
            }
        }
    }
}
