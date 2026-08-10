package io.github.manugh.xg2g.android.recordings

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.webkit.CookieManager
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
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
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.github.manugh.xg2g.android.R
import io.github.manugh.xg2g.android.dashboard.ModuleState
import io.github.manugh.xg2g.android.playback.net.CookieBackedAuthSession
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

@Composable
internal fun RecordingsScreen(
    viewModel: RecordingsViewModel,
    onPlayRecording: (RecordingItem) -> Unit,
    onOpenSetup: (() -> Unit)? = null
) {
    val state by viewModel.uiState.collectAsState()
    val gridState = rememberLazyGridState()
    val continueListState = rememberLazyListState()
    var lastFocusedId by rememberSaveable { mutableStateOf<String?>(null) }
    val focusRequesters = remember { mutableMapOf<String, FocusRequester>() }

    Surface(
        modifier = Modifier.fillMaxSize(),
        color = colorResource(R.color.color_bg_base)
    ) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 28.dp, vertical = 20.dp)
        ) {
            // Screen Header
            RecordingsHeader(
                serverLabel = state.serverLabel,
                currentRoot = state.selectedRoot ?: state.roots.firstOrNull()?.name ?: "movie",
                currentPath = state.currentPath,
                onRefresh = { viewModel.refresh(isInitial = false) }
            )

            Spacer(modifier = Modifier.height(12.dp))

            // Navigation Bar: Storage Roots & Breadcrumbs Trail
            PathNavigationBar(
                roots = state.roots,
                selectedRoot = state.selectedRoot,
                breadcrumbs = state.breadcrumbs,
                currentPath = state.currentPath,
                onSelectRoot = { viewModel.selectRoot(it) },
                onNavigateBreadcrumb = { viewModel.navigateToBreadcrumb(it) },
                onNavigateUp = { viewModel.navigateUp() }
            )

            Spacer(modifier = Modifier.height(14.dp))

            when (val recState = state.recordingsState) {
                is ModuleState.Loading -> {
                    Box(
                        modifier = Modifier.fillMaxSize(),
                        contentAlignment = Alignment.Center
                    ) {
                        CircularProgressIndicator(color = colorResource(R.color.color_action))
                    }
                }
                is ModuleState.Error -> {
                    RecordingsErrorView(
                        message = recState.message.orEmpty(),
                        onRetry = { viewModel.refresh(isInitial = true) },
                        onOpenSetup = onOpenSetup
                    )
                }
                is ModuleState.Empty -> {
                    // Check if we have directories even if recordings is empty
                    if (state.directories.isNotEmpty()) {
                        DirectoryGridSection(
                            directories = state.directories,
                            onSelectDirectory = { viewModel.navigateToDirectory(it) }
                        )
                    } else {
                        Box(
                            modifier = Modifier.fillMaxSize(),
                            contentAlignment = Alignment.Center
                        ) {
                            Column(
                                horizontalAlignment = Alignment.CenterHorizontally,
                                verticalArrangement = Arrangement.spacedBy(10.dp)
                            ) {
                                Text(
                                    text = if (state.currentPath.isNotEmpty()) {
                                        "Keine Aufnahmen oder Unterordner in diesem Pfad."
                                    } else {
                                        "Keine Aufnahmen auf dem Receiver vorhanden."
                                    },
                                    style = MaterialTheme.typography.bodyLarge,
                                    color = colorResource(R.color.color_text_secondary)
                                )

                                if (state.currentPath.isNotEmpty()) {
                                    TvActionButton(
                                        label = "Zurück zum Hauptverzeichnis",
                                        onClick = { viewModel.navigateToBreadcrumb("") },
                                        isPrimary = true
                                    )
                                }
                            }
                        }
                    }
                }
                is ModuleState.Success -> {
                    val recordings = recState.data

                    // Restore focus when returning to screen
                    LaunchedEffect(recordings, lastFocusedId) {
                        lastFocusedId?.let { id ->
                            focusRequesters[id]?.requestFocus()
                        }
                    }

                    LazyVerticalGrid(
                        columns = GridCells.Fixed(4),
                        state = gridState,
                        horizontalArrangement = Arrangement.spacedBy(16.dp),
                        verticalArrangement = Arrangement.spacedBy(16.dp),
                        contentPadding = PaddingValues(bottom = 32.dp),
                        modifier = Modifier.fillMaxSize()
                    ) {
                        // Section: Directories / Folders (if any)
                        if (state.directories.isNotEmpty()) {
                            item(span = { GridItemSpan(maxLineSpan) }) {
                                Column {
                                    Text(
                                        text = "ORDNER (${state.directories.size})",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = colorResource(R.color.color_text_secondary),
                                        fontWeight = FontWeight.Bold,
                                        letterSpacing = 1.2.sp
                                    )
                                    Spacer(modifier = Modifier.height(8.dp))
                                    LazyRow(
                                        horizontalArrangement = Arrangement.spacedBy(14.dp),
                                        contentPadding = PaddingValues(bottom = 6.dp)
                                    ) {
                                        items(state.directories, key = { "dir_${it.path}" }) { dir ->
                                            DirectoryCard(
                                                item = dir,
                                                onClick = { viewModel.navigateToDirectory(dir.path) }
                                            )
                                        }
                                    }
                                    Spacer(modifier = Modifier.height(10.dp))
                                }
                            }
                        }

                        // Section: Continue Watching Shelf (if at root and has items)
                        val continueState = state.continueWatchingState
                        if (continueState is ModuleState.Success && continueState.data.isNotEmpty() && state.currentPath.isEmpty()) {
                            item(span = { GridItemSpan(maxLineSpan) }) {
                                Column {
                                    Text(
                                        text = "WEITERANSCHAUEN",
                                        style = MaterialTheme.typography.labelSmall,
                                        color = colorResource(R.color.color_live),
                                        fontWeight = FontWeight.Bold,
                                        letterSpacing = 1.2.sp
                                    )
                                    Spacer(modifier = Modifier.height(8.dp))

                                    LazyRow(
                                        state = continueListState,
                                        horizontalArrangement = Arrangement.spacedBy(16.dp),
                                        contentPadding = PaddingValues(bottom = 8.dp)
                                    ) {
                                        items(continueState.data, key = { "continue_${it.recordingId}" }) { item ->
                                            val requester = focusRequesters.getOrPut("continue_${item.recordingId}") { FocusRequester() }
                                            RecordingCard(
                                                item = item,
                                                baseUrl = state.baseUrl,
                                                onClick = {
                                                    lastFocusedId = "continue_${item.recordingId}"
                                                    onPlayRecording(item)
                                                },
                                                onFocused = { lastFocusedId = "continue_${item.recordingId}" },
                                                focusRequester = requester,
                                                isFeatured = true
                                            )
                                        }
                                    }
                                    Spacer(modifier = Modifier.height(10.dp))
                                }
                            }
                        }

                        // Section: Recordings Grid Header
                        item(span = { GridItemSpan(maxLineSpan) }) {
                            Text(
                                text = "AUFNAHMEN (${recordings.size})",
                                style = MaterialTheme.typography.labelSmall,
                                color = colorResource(R.color.color_text_secondary),
                                fontWeight = FontWeight.Bold,
                                letterSpacing = 1.2.sp
                            )
                        }

                        // Section: Recordings Grid Items
                        items(recordings, key = { it.recordingId }) { item ->
                            val requester = focusRequesters.getOrPut(item.recordingId) { FocusRequester() }
                            RecordingCard(
                                item = item,
                                baseUrl = state.baseUrl,
                                onClick = {
                                    lastFocusedId = item.recordingId
                                    onPlayRecording(item)
                                },
                                onFocused = { lastFocusedId = item.recordingId },
                                focusRequester = requester,
                                isFeatured = false
                            )
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun PathNavigationBar(
    roots: List<RecordingRoot>,
    selectedRoot: String?,
    breadcrumbs: List<Breadcrumb>,
    currentPath: String,
    onSelectRoot: (String) -> Unit,
    onNavigateBreadcrumb: (String) -> Unit,
    onNavigateUp: () -> Unit
) {
    val activeRootName = roots.find { it.id == selectedRoot }?.name ?: selectedRoot ?: "movie"

    Surface(
        shape = RoundedCornerShape(10.dp),
        color = colorResource(R.color.color_bg_elevated),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle)),
        modifier = Modifier.fillMaxWidth()
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 14.dp, vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            // Storage Location Selector (roots from OpenWebif)
            if (roots.isNotEmpty()) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp)
                ) {
                    Text(
                        text = "Speicherort:",
                        style = MaterialTheme.typography.labelSmall,
                        color = colorResource(R.color.color_text_secondary),
                        fontWeight = FontWeight.Bold
                    )
                    roots.forEach { r ->
                        val isSelected = (r.id == selectedRoot) || (selectedRoot == null && r.id == roots.firstOrNull()?.id)
                        RootChip(
                            label = "💾 ${r.name.ifBlank { r.id }}",
                            isSelected = isSelected,
                            onClick = { onSelectRoot(r.id) }
                        )
                    }
                }
                Spacer(modifier = Modifier.width(4.dp))
                Text(
                    text = "|",
                    color = colorResource(R.color.color_border_subtle),
                    style = MaterialTheme.typography.bodyMedium
                )
                Spacer(modifier = Modifier.width(4.dp))
            }

            // Up Button (when inside a subfolder)
            if (currentPath.isNotEmpty()) {
                TvActionButton(
                    label = "⬅ Ebene hoch",
                    onClick = onNavigateUp,
                    isPrimary = false
                )
            }

            // Breadcrumbs Trail with real root name
            LazyRow(
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                modifier = Modifier.weight(1f)
            ) {
                item {
                    BreadcrumbChip(
                        label = activeRootName,
                        isCurrent = currentPath.isEmpty() && breadcrumbs.isEmpty(),
                        onClick = { onNavigateBreadcrumb("") }
                    )
                }

                items(breadcrumbs) { crumb ->
                    Text(
                        text = "/",
                        color = colorResource(R.color.color_text_secondary),
                        style = MaterialTheme.typography.bodyMedium
                    )
                    BreadcrumbChip(
                        label = crumb.name,
                        isCurrent = crumb.path == currentPath,
                        onClick = { onNavigateBreadcrumb(crumb.path) }
                    )
                }
            }
        }
    }
}

@Composable
private fun DirectoryGridSection(
    directories: List<DirectoryItem>,
    onSelectDirectory: (String) -> Unit
) {
    Column {
        Text(
            text = "ORDNER (${directories.size})",
            style = MaterialTheme.typography.labelSmall,
            color = colorResource(R.color.color_text_secondary),
            fontWeight = FontWeight.Bold,
            letterSpacing = 1.2.sp
        )
        Spacer(modifier = Modifier.height(12.dp))
        LazyVerticalGrid(
            columns = GridCells.Fixed(4),
            horizontalArrangement = Arrangement.spacedBy(16.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp)
        ) {
            items(directories, key = { "dir_${it.path}" }) { dir ->
                DirectoryCard(
                    item = dir,
                    onClick = { onSelectDirectory(dir.path) }
                )
            }
        }
    }
}

@Composable
private fun DirectoryCard(
    item: DirectoryItem,
    onClick: () -> Unit
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    Surface(
        modifier = Modifier
            .width(190.dp)
            .clip(RoundedCornerShape(12.dp))
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick
            )
            .focusable(interactionSource = interactionSource),
        shape = RoundedCornerShape(12.dp),
        color = colorResource(R.color.color_bg_elevated),
        border = BorderStroke(
            width = if (isFocused) 2.dp else 1.dp,
            color = if (isFocused) colorResource(R.color.color_action) else colorResource(R.color.color_border_subtle)
        )
    ) {
        Row(
            modifier = Modifier.padding(14.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            Text(
                text = "📁",
                fontSize = 24.sp
            )
            Column {
                Text(
                    text = item.name,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold,
                    color = colorResource(R.color.color_text_primary),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )
                Text(
                    text = "Ordner öffnen",
                    style = MaterialTheme.typography.labelSmall,
                    color = colorResource(R.color.color_text_secondary),
                    fontSize = 11.sp
                )
            }
        }
    }
}

@Composable
private fun RootChip(
    label: String,
    isSelected: Boolean,
    onClick: () -> Unit
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    Surface(
        modifier = Modifier
            .clip(RoundedCornerShape(6.dp))
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick
            )
            .focusable(interactionSource = interactionSource),
        shape = RoundedCornerShape(6.dp),
        color = if (isSelected) colorResource(R.color.color_action) else Color(0xFF1E293B),
        border = BorderStroke(
            width = if (isFocused) 2.dp else 1.dp,
            color = if (isFocused) Color.White else colorResource(R.color.color_border_subtle)
        )
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            fontWeight = FontWeight.Bold,
            color = Color.White,
            modifier = Modifier.padding(horizontal = 10.dp, vertical = 5.dp)
        )
    }
}

@Composable
private fun BreadcrumbChip(
    label: String,
    isCurrent: Boolean,
    onClick: () -> Unit
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    Surface(
        modifier = Modifier
            .clip(RoundedCornerShape(6.dp))
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick
            )
            .focusable(interactionSource = interactionSource),
        shape = RoundedCornerShape(6.dp),
        color = if (isCurrent) Color(0xFF334155) else Color.Transparent,
        border = BorderStroke(
            width = if (isFocused) 2.dp else if (isCurrent) 1.dp else 0.dp,
            color = if (isFocused) Color.White else if (isCurrent) colorResource(R.color.color_border_subtle) else Color.Transparent
        )
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            fontWeight = if (isCurrent) FontWeight.Bold else FontWeight.Normal,
            color = if (isCurrent) Color.White else colorResource(R.color.color_text_secondary),
            modifier = Modifier.padding(horizontal = 8.dp, vertical = 4.dp)
        )
    }
}

@Composable
private fun RecordingsHeader(
    serverLabel: String,
    currentRoot: String,
    currentPath: String,
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
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = "Aufnahmen",
                    style = MaterialTheme.typography.headlineMedium,
                    fontWeight = FontWeight.Bold,
                    color = colorResource(R.color.color_text_primary)
                )
                Text(
                    text = if (currentPath.isNotBlank()) "in $currentRoot / $currentPath" else "in $currentRoot",
                    style = MaterialTheme.typography.bodyMedium,
                    color = colorResource(R.color.color_text_secondary)
                )
            }
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
private fun RecordingsErrorView(
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
                text = if (isAuthError) "Anmeldung erforderlich, um Aufnahmen abzurufen." else message,
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
private fun RecordingCard(
    item: RecordingItem,
    baseUrl: String,
    onClick: () -> Unit,
    onFocused: (() -> Unit)? = null,
    focusRequester: FocusRequester? = null,
    isFeatured: Boolean
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    LaunchedEffect(isFocused) {
        if (isFocused) {
            onFocused?.invoke()
        }
    }

    val width = if (isFeatured) 220.dp else 190.dp
    val height = if (isFeatured) 140.dp else 125.dp

    val progressFraction = remember(item) { getResumeProgressFraction(item) }

    Surface(
        modifier = Modifier
            .width(width)
            .clip(RoundedCornerShape(12.dp))
            .let { mod ->
                if (focusRequester != null) mod.focusRequester(focusRequester) else mod
            }
            .clickable(
                interactionSource = interactionSource,
                indication = null,
                onClick = onClick
            )
            .focusable(interactionSource = interactionSource),
        shape = RoundedCornerShape(12.dp),
        color = colorResource(R.color.color_bg_elevated),
        border = BorderStroke(
            width = if (isFocused) 2.dp else 1.dp,
            color = if (isFocused) colorResource(R.color.color_action) else colorResource(R.color.color_border_subtle)
        )
    ) {
        Column {
            // Poster Artwork / Fallback
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(height)
                    .background(
                        Brush.verticalGradient(
                            colors = listOf(
                                Color(0xFF1E293B),
                                Color(0xFF0F172A)
                            )
                        )
                    )
            ) {
                AuthenticatedThumbnailImage(
                    baseUrl = baseUrl,
                    recordingId = item.recordingId,
                    title = item.title ?: "Aufnahme"
                )

                // Length chip top right
                val durationText = item.length ?: formatDuration(item.durationSeconds)
                if (durationText.isNotBlank()) {
                    Surface(
                        shape = RoundedCornerShape(4.dp),
                        color = Color.Black.copy(alpha = 0.75f),
                        modifier = Modifier
                            .align(Alignment.TopEnd)
                            .padding(6.dp)
                    ) {
                        Text(
                            text = durationText,
                            style = MaterialTheme.typography.labelSmall,
                            color = Color.White,
                            fontSize = 10.sp,
                            modifier = Modifier.padding(horizontal = 6.dp, vertical = 2.dp)
                        )
                    }
                }

                // Progress Bar at bottom of poster
                if (progressFraction != null && progressFraction > 0f) {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .height(3.dp)
                            .align(Alignment.BottomStart)
                            .background(Color.White.copy(alpha = 0.2f))
                    ) {
                        Box(
                            modifier = Modifier
                                .fillMaxWidth(progressFraction)
                                .height(3.dp)
                                .background(colorResource(R.color.color_live))
                        )
                    }
                }
            }

            // Card Text Content
            Column(
                modifier = Modifier.padding(10.dp)
            ) {
                Text(
                    text = item.title ?: "Unbenannte Aufnahme",
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold,
                    color = colorResource(R.color.color_text_primary),
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
                )

                val formattedDate = formatDate(item.beginUnixSeconds)
                if (formattedDate.isNotBlank()) {
                    Spacer(modifier = Modifier.height(2.dp))
                    Text(
                        text = formattedDate,
                        style = MaterialTheme.typography.labelSmall,
                        color = colorResource(R.color.color_text_secondary),
                        fontSize = 11.sp,
                        maxLines = 1
                    )
                }
            }
        }
    }
}

@Composable
private fun AuthenticatedThumbnailImage(
    baseUrl: String,
    recordingId: String,
    title: String
) {
    var bitmap by remember(recordingId) { mutableStateOf<Bitmap?>(null) }
    var hasError by remember(recordingId) { mutableStateOf(false) }

    LaunchedEffect(recordingId, baseUrl) {
        withContext(Dispatchers.IO) {
            try {
                val cookieSession = CookieBackedAuthSession(CookieManager.getInstance())
                val urlStr = "$baseUrl/api/v3/recordings/${recordingId}/thumbnail.jpg"
                val httpUrl = urlStr.toHttpUrlOrNull()
                if (httpUrl == null) {
                    hasError = true
                    return@withContext
                }

                val client = OkHttpClient.Builder()
                    .addNetworkInterceptor { chain ->
                        val original = chain.request()
                        val builder = original.newBuilder()
                        cookieSession.applyCookies(original.url, builder)
                        val response = chain.proceed(builder.build())
                        cookieSession.storeCookies(original.url, response.headers)
                        response
                    }
                    .build()

                val request = Request.Builder()
                    .url(httpUrl)
                    .get()
                    .build()
                    .withSameOriginHeaders(httpUrl)

                val response = client.newCall(request).execute()
                response.use { res ->
                    if (res.isSuccessful) {
                        val bytes = res.body?.bytes()
                        if (bytes != null && bytes.isNotEmpty()) {
                            bitmap = BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
                        } else {
                            hasError = true
                        }
                    } else {
                        hasError = true
                    }
                }
            } catch (_: Exception) {
                hasError = true
            }
        }
    }

    if (bitmap != null) {
        Image(
            bitmap = bitmap!!.asImageBitmap(),
            contentDescription = title,
            contentScale = ContentScale.Crop,
            modifier = Modifier.fillMaxSize()
        )
    } else {
        Box(
            modifier = Modifier.fillMaxSize(),
            contentAlignment = Alignment.Center
        ) {
            val initials = remember(title) {
                title.split(" ")
                    .filter { it.isNotBlank() }
                    .take(2)
                    .map { it.first().uppercaseChar() }
                    .joinToString("")
                    .ifBlank { "REC" }
            }
            Text(
                text = initials,
                style = MaterialTheme.typography.titleLarge,
                fontWeight = FontWeight.Black,
                color = Color.White.copy(alpha = 0.25f),
                letterSpacing = 2.sp
            )
        }
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
        color = if (isPrimary) colorResource(R.color.color_action) else Color(0xFF1E293B),
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
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 6.dp)
        )
    }
}

private fun getResumeProgressFraction(item: RecordingItem): Float? {
    val r = item.resume ?: return null
    val dur = r.durationSeconds ?: item.durationSeconds ?: return null
    if (dur <= 0L || r.posSeconds <= 0L) return null
    return (r.posSeconds.toFloat() / dur.toFloat()).coerceIn(0f, 1f)
}

private fun formatDuration(seconds: Long?): String {
    if (seconds == null || seconds <= 0L) return ""
    val minutes = seconds / 60
    return if (minutes >= 60) {
        val hours = minutes / 60
        val remMin = minutes % 60
        "${hours}h ${remMin}m"
    } else {
        "${minutes}m"
    }
}

private fun formatDate(epochSeconds: Long?): String {
    if (epochSeconds == null || epochSeconds <= 0L) return ""
    val sdf = SimpleDateFormat("dd.MM.yyyy • HH:mm", Locale.GERMANY)
    return sdf.format(Date(epochSeconds * 1000L))
}
