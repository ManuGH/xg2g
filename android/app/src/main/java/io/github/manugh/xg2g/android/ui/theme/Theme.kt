package io.github.manugh.xg2g.android.ui.theme

import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.colorResource
import io.github.manugh.xg2g.android.R

@Composable
fun GuideTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = MaterialTheme.colorScheme.copy(
            primary = colorResource(R.color.color_action),
            secondary = colorResource(R.color.color_live),
            surface = colorResource(R.color.color_bg_elevated),
            surfaceVariant = colorResource(R.color.color_surface_shell_strong),
            background = colorResource(R.color.color_bg_base),
            onBackground = colorResource(R.color.color_text_primary),
            onSurface = colorResource(R.color.color_text_primary),
            onSurfaceVariant = colorResource(R.color.color_text_secondary),
            outline = colorResource(R.color.color_border_base),
            outlineVariant = colorResource(R.color.color_border_subtle),
            error = colorResource(R.color.color_status_error)
        ),
        typography = BroadcastTypography,
        shapes = BroadcastShapes,
        content = content
    )
}
