package io.github.manugh.xg2g.android.settings

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.focusable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.colorResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import io.github.manugh.xg2g.android.R

@Composable
internal fun SettingsScreen(
    viewModel: SettingsViewModel,
    onChangeServer: () -> Unit,
    onBack: (() -> Unit)? = null
) {
    val state by viewModel.uiState.collectAsState()
    var showTokenDialog by remember { mutableStateOf(false) }
    var tokenEditValue by remember { mutableStateOf("") }
    val initialFocusRequester = remember { FocusRequester() }

    LaunchedEffect(Unit) {
        initialFocusRequester.requestFocus()
    }

    Surface(
        modifier = Modifier.fillMaxSize(),
        color = colorResource(R.color.color_bg_base)
    ) {
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 32.dp, vertical = 24.dp),
            verticalArrangement = Arrangement.spacedBy(18.dp)
        ) {
            // Header
            item {
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
                        Text(
                            text = "Einstellungen & System",
                            style = MaterialTheme.typography.headlineMedium,
                            fontWeight = FontWeight.Bold,
                            color = colorResource(R.color.color_text_primary)
                        )
                    }

                    if (onBack != null) {
                        TvSettingsButton(
                            label = "Zurück zum Dashboard",
                            onClick = onBack,
                            isPrimary = false
                        )
                    }
                }
            }

            // Section 1: Server Connection
            item {
                SettingsSectionTitle(title = "SERVER-VERBINDUNG")
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = "Aktive Server-URL",
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.Bold,
                                color = colorResource(R.color.color_text_primary)
                            )
                            Spacer(modifier = Modifier.height(4.dp))
                            Text(
                                text = state.serverUrl.ifBlank { "Keine URL konfiguriert" },
                                style = MaterialTheme.typography.bodyMedium,
                                color = colorResource(R.color.color_text_secondary)
                            )
                        }

                        Spacer(modifier = Modifier.width(16.dp))

                        TvSettingsButton(
                            label = "Server ändern",
                            onClick = onChangeServer,
                            isPrimary = false,
                            focusRequester = initialFocusRequester
                        )
                    }
                }
            }

            // Section 2: API Token / Authentication
            item {
                SettingsSectionTitle(title = "AUTHENTIFIZIERUNG & API-TOKEN")
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Column {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    text = "xg2g API-Token",
                                    style = MaterialTheme.typography.titleSmall,
                                    fontWeight = FontWeight.Bold,
                                    color = colorResource(R.color.color_text_primary)
                                )
                                Spacer(modifier = Modifier.height(2.dp))
                                Text(
                                    text = if (!state.authToken.isNullOrBlank()) {
                                        "Token: ${state.authToken.orEmpty().take(3)}•••••••• (Aktiv)"
                                    } else {
                                        "Kein Token hinterlegt"
                                    },
                                    style = MaterialTheme.typography.bodyMedium,
                                    color = colorResource(R.color.color_text_secondary)
                                )
                            }

                            val hasToken = !state.authToken.isNullOrBlank()
                            Surface(
                                shape = RoundedCornerShape(4.dp),
                                color = if (hasToken) Color(0xFF065F46) else Color(0xFF78350F)
                            ) {
                                Text(
                                    text = if (hasToken) "AUTHENTIFIZIERT" else "KEIN TOKEN",
                                    style = MaterialTheme.typography.labelSmall,
                                    color = if (hasToken) Color(0xFF34D399) else Color(0xFFFBBF24),
                                    fontSize = 10.sp,
                                    fontWeight = FontWeight.Bold,
                                    modifier = Modifier.padding(horizontal = 8.dp, vertical = 3.dp)
                                )
                            }
                        }

                        Spacer(modifier = Modifier.height(14.dp))

                        if (state.isPairingActive) {
                            Surface(
                                shape = RoundedCornerShape(8.dp),
                                color = Color(0xFF1E293B),
                                border = BorderStroke(1.dp, Color(0xFF3B82F6)),
                                modifier = Modifier.fillMaxWidth().padding(vertical = 6.dp)
                            ) {
                                Column(
                                    modifier = Modifier.padding(16.dp),
                                    horizontalAlignment = Alignment.CenterHorizontally,
                                    verticalArrangement = Arrangement.spacedBy(8.dp)
                                ) {
                                    Text(
                                        text = "📱 GERÄT KOPPELN (DEVICE PAIRING)",
                                        style = MaterialTheme.typography.labelMedium,
                                        fontWeight = FontWeight.Bold,
                                        color = Color(0xFF60A5FA)
                                    )
                                    Text(
                                        text = "Gib diesen PIN in der WebUI unter Einstellungen > Android TV ein:",
                                        style = MaterialTheme.typography.bodyMedium,
                                        color = Color.White
                                    )
                                    Surface(
                                        shape = RoundedCornerShape(6.dp),
                                        color = Color(0xFF0F172A),
                                        border = BorderStroke(1.dp, Color(0xFF60A5FA))
                                    ) {
                                        Text(
                                            text = state.pairingCode ?: "Generiere PIN...",
                                            style = MaterialTheme.typography.headlineLarge,
                                            fontWeight = FontWeight.Black,
                                            color = Color(0xFF38BDF8),
                                            letterSpacing = 4.sp,
                                            modifier = Modifier.padding(horizontal = 24.dp, vertical = 10.dp)
                                        )
                                    }
                                    Text(
                                        text = "Warte auf Bestätigung in der WebUI...",
                                        style = MaterialTheme.typography.bodySmall,
                                        color = colorResource(R.color.color_text_secondary)
                                    )
                                    if (state.pairingError != null) {
                                        Text(
                                            text = state.pairingError.orEmpty(),
                                            style = MaterialTheme.typography.bodySmall,
                                            color = Color(0xFFEF4444)
                                        )
                                    }
                                    Spacer(modifier = Modifier.height(4.dp))
                                    TvSettingsButton(
                                        label = "Kopplung abbrechen",
                                        onClick = { viewModel.cancelPairing() },
                                        isPrimary = false
                                    )
                                }
                            }
                        } else {
                            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                TvSettingsButton(
                                    label = "📱 TV mit WebUI koppeln (PIN / QR)",
                                    onClick = { viewModel.startPairing() },
                                    isPrimary = true
                                )

                                TvSettingsButton(
                                    label = if (state.authToken.isNullOrBlank()) "Token manuell eingeben" else "Manuell bearbeiten",
                                    onClick = {
                                        tokenEditValue = state.authToken.orEmpty()
                                        showTokenDialog = true
                                    },
                                    isPrimary = false
                                )

                                if (!state.authToken.isNullOrBlank()) {
                                    TvSettingsButton(
                                        label = "Token löschen",
                                        onClick = { viewModel.saveToken(null) },
                                        isPrimary = false
                                    )
                                }
                            }
                        }

                        if (state.message != null) {
                            Spacer(modifier = Modifier.height(8.dp))
                            Text(
                                text = state.message.orEmpty(),
                                style = MaterialTheme.typography.bodySmall,
                                color = Color(0xFF34D399)
                            )
                        }
                    }
                }
            }

            // Section 3: Diagnostics & System Status
            item {
                SettingsSectionTitle(title = "SYSTEMSTATUS & DIAGNOSE")
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        DiagnosticRow(label = "Vu+ Receiver Status", value = state.receiverStatus)
                        DiagnosticRow(label = "EPG & Kanäle", value = state.epgStatus)
                        DiagnosticRow(label = "App Engine", value = state.appVersion)

                        Spacer(modifier = Modifier.height(4.dp))

                        TvSettingsButton(
                            label = "Status jetzt aktualisieren",
                            onClick = { viewModel.refreshHealth() },
                            isPrimary = false
                        )
                    }
                }
            }

            item {
                Spacer(modifier = Modifier.height(30.dp))
            }
        }
    }

    // Modal Dialog for Token Input
    if (showTokenDialog) {
        AlertDialog(
            onDismissRequest = { showTokenDialog = false },
            title = {
                Text(
                    text = "API-Token konfigurieren",
                    fontWeight = FontWeight.Bold,
                    color = Color.White
                )
            },
            text = {
                Column {
                    Text(
                        text = "Gib den Server-Token für xg2g ein (z.B. test04):",
                        style = MaterialTheme.typography.bodyMedium,
                        color = Color(0xFFCBD5E1)
                    )
                    Spacer(modifier = Modifier.height(12.dp))
                    OutlinedTextField(
                        value = tokenEditValue,
                        onValueChange = { tokenEditValue = it },
                        modifier = Modifier.fillMaxWidth(),
                        singleLine = true,
                        colors = OutlinedTextFieldDefaults.colors(
                            focusedBorderColor = colorResource(R.color.color_action),
                            unfocusedBorderColor = colorResource(R.color.color_border_subtle),
                            focusedTextColor = Color.White,
                            unfocusedTextColor = Color.White
                        )
                    )
                }
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        viewModel.saveToken(tokenEditValue)
                        showTokenDialog = false
                    }
                ) {
                    Text("Speichern", color = colorResource(R.color.color_action), fontWeight = FontWeight.Bold)
                }
            },
            dismissButton = {
                TextButton(onClick = { showTokenDialog = false }) {
                    Text("Abbrechen", color = Color.White)
                }
            },
            containerColor = colorResource(R.color.color_bg_elevated),
            shape = RoundedCornerShape(16.dp)
        )
    }
}

@Composable
private fun SettingsSectionTitle(title: String) {
    Text(
        text = title,
        style = MaterialTheme.typography.labelSmall,
        color = colorResource(R.color.color_text_secondary),
        fontWeight = FontWeight.Bold,
        letterSpacing = 1.2.sp
    )
}

@Composable
private fun SettingsCard(content: @Composable () -> Unit) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(12.dp),
        color = colorResource(R.color.color_bg_elevated),
        border = BorderStroke(1.dp, colorResource(R.color.color_border_subtle))
    ) {
        Box(modifier = Modifier.padding(18.dp)) {
            content()
        }
    }
}

@Composable
private fun DiagnosticRow(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = colorResource(R.color.color_text_secondary)
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Bold,
            color = if (value.contains("ONLINE") || value.contains("AKTIV")) Color(0xFF34D399) else Color.White
        )
    }
}

@Composable
private fun TvSettingsButton(
    label: String,
    onClick: () -> Unit,
    isPrimary: Boolean,
    focusRequester: FocusRequester? = null
) {
    val interactionSource = remember { MutableInteractionSource() }
    val isFocused by interactionSource.collectIsFocusedAsState()

    Surface(
        modifier = Modifier
            .clip(RoundedCornerShape(8.dp))
            .let { mod ->
                if (focusRequester != null) mod.focusRequester(focusRequester) else mod
            }
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
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp)
        )
    }
}
