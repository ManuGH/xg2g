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
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.LinearProgressIndicator
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
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
    var showPinDialog by remember { mutableStateOf(false) }
    var pinEditValue by remember { mutableStateOf("") }
    val initialFocusRequester = remember { FocusRequester() }

    val isAdminAllowed = !state.pinConfigured || state.isUnlocked

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
                            text = stringResource(R.string.brand_console),
                            style = MaterialTheme.typography.labelSmall,
                            color = colorResource(R.color.color_text_secondary),
                            letterSpacing = 1.5.sp
                        )
                        Text(
                            text = stringResource(R.string.settings_header_title),
                            style = MaterialTheme.typography.headlineMedium,
                            fontWeight = FontWeight.Bold,
                            color = colorResource(R.color.color_text_primary)
                        )
                    }

                    if (onBack != null) {
                        TvSettingsButton(
                            label = stringResource(R.string.settings_back_to_dashboard),
                            onClick = onBack,
                            isPrimary = false
                        )
                    }
                }
            }

            // Admin Mode / Household PIN Security Status
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_admin_pin))
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                Text(
                                    text = if (state.pinConfigured) {
                                        if (state.isUnlocked) stringResource(R.string.settings_admin_unlocked) else stringResource(R.string.settings_admin_locked)
                                    } else {
                                        stringResource(R.string.settings_pin_not_set)
                                    },
                                    style = MaterialTheme.typography.titleSmall,
                                    fontWeight = FontWeight.Bold,
                                    color = if (state.isUnlocked || !state.pinConfigured) Color(0xFF34D399) else Color(0xFFFBBF24)
                                )
                            }
                            Spacer(modifier = Modifier.height(4.dp))
                            Text(
                                text = if (state.pinConfigured) {
                                    if (state.isUnlocked) {
                                        stringResource(R.string.settings_admin_desc_unlocked)
                                    } else {
                                        stringResource(R.string.settings_admin_desc_locked)
                                    }
                                } else {
                                    stringResource(R.string.settings_pin_desc_not_set)
                                },
                                style = MaterialTheme.typography.bodyMedium,
                                color = colorResource(R.color.color_text_secondary)
                            )
                        }

                        Spacer(modifier = Modifier.width(16.dp))

                        if (state.pinConfigured) {
                            if (state.isUnlocked) {
                                TvSettingsButton(
                                    label = stringResource(R.string.settings_action_lock),
                                    onClick = { viewModel.lockAdminMode() },
                                    isPrimary = false
                                )
                            } else {
                                TvSettingsButton(
                                    label = stringResource(R.string.settings_action_unlock_pin),
                                    onClick = {
                                        pinEditValue = ""
                                        showPinDialog = true
                                    },
                                    isPrimary = true,
                                    focusRequester = initialFocusRequester
                                )
                            }
                        }
                    }
                }
            }

            // Household Profiles Selection Section
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_household_profiles))
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        Text(
                            text = stringResource(R.string.settings_active_household_profile),
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.Bold,
                            color = colorResource(R.color.color_text_primary)
                        )
                        Text(
                            text = stringResource(R.string.settings_household_profiles_desc),
                            style = MaterialTheme.typography.bodySmall,
                            color = colorResource(R.color.color_text_secondary)
                        )
                        Spacer(modifier = Modifier.height(6.dp))

                        if (state.profiles.isEmpty()) {
                            Text(
                                text = stringResource(R.string.settings_default_profile_active),
                                style = MaterialTheme.typography.bodyMedium,
                                color = colorResource(R.color.color_text_secondary)
                            )
                        } else {
                            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                                state.profiles.forEach { profile ->
                                    val isSelected = state.selectedProfileId == profile.id
                                    val icon = if (profile.kind == "child") "🧒" else "👤"
                                    TvSettingsOptionButton(
                                        label = "$icon ${profile.name}",
                                        isSelected = isSelected,
                                        onClick = {
                                            if (profile.kind == "adult" && state.pinConfigured && !state.isUnlocked && !isSelected) {
                                                showPinDialog = true
                                            } else {
                                                viewModel.selectHouseholdProfile(profile.id)
                                            }
                                        }
                                    )
                                }
                            }
                        }
                    }
                }
            }

            // Section 1: Server Connection
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_server))
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = stringResource(R.string.settings_active_server_url),
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.Bold,
                                color = colorResource(R.color.color_text_primary)
                            )
                            Spacer(modifier = Modifier.height(4.dp))
                            Text(
                                text = state.serverUrl.ifBlank { stringResource(R.string.settings_no_server_configured) },
                                style = MaterialTheme.typography.bodyMedium,
                                color = colorResource(R.color.color_text_secondary)
                            )
                        }

                        Spacer(modifier = Modifier.width(16.dp))

                        TvSettingsButton(
                            label = stringResource(R.string.settings_change_server),
                            onClick = {
                                if (isAdminAllowed) {
                                    onChangeServer()
                                } else {
                                    showPinDialog = true
                                }
                            },
                            isPrimary = false,
                            focusRequester = if (!state.pinConfigured || state.isUnlocked) initialFocusRequester else null
                        )
                    }
                }
            }

            // Section 2: API Token / Authentication
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_auth_token))
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
                                    text = stringResource(R.string.settings_api_token_title),
                                    style = MaterialTheme.typography.titleSmall,
                                    fontWeight = FontWeight.Bold,
                                    color = colorResource(R.color.color_text_primary)
                                )
                                Spacer(modifier = Modifier.height(2.dp))
                                Text(
                                    text = if (!state.authToken.isNullOrBlank()) {
                                        stringResource(R.string.settings_token_active, state.authToken.orEmpty().take(3))
                                    } else {
                                        stringResource(R.string.settings_no_token)
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
                                    text = if (hasToken) stringResource(R.string.settings_status_authenticated) else stringResource(R.string.settings_status_no_token),
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
                                        text = stringResource(R.string.settings_pairing_title),
                                        style = MaterialTheme.typography.labelMedium,
                                        fontWeight = FontWeight.Bold,
                                        color = Color(0xFF60A5FA)
                                    )
                                    Text(
                                        text = stringResource(R.string.settings_pairing_desc),
                                        style = MaterialTheme.typography.bodyMedium,
                                        color = Color.White
                                    )
                                    Surface(
                                        shape = RoundedCornerShape(6.dp),
                                        color = Color(0xFF0F172A),
                                        border = BorderStroke(1.dp, Color(0xFF60A5FA))
                                    ) {
                                        Text(
                                            text = state.pairingCode ?: stringResource(R.string.settings_pairing_generating),
                                            style = MaterialTheme.typography.headlineLarge,
                                            fontWeight = FontWeight.Black,
                                            color = Color(0xFF38BDF8),
                                            letterSpacing = 4.sp,
                                            modifier = Modifier.padding(horizontal = 24.dp, vertical = 10.dp)
                                        )
                                    }
                                    Text(
                                        text = stringResource(R.string.settings_pairing_waiting),
                                        style = MaterialTheme.typography.bodySmall,
                                        color = colorResource(R.color.color_text_secondary)
                                    )
                                    if (state.pairingError != null) {
                                        Text(
                                            text = formatPairingError(state.pairingError.orEmpty()),
                                            style = MaterialTheme.typography.bodySmall,
                                            color = Color(0xFFEF4444)
                                        )
                                    }
                                    Spacer(modifier = Modifier.height(4.dp))
                                    TvSettingsButton(
                                        label = stringResource(R.string.settings_pairing_cancel),
                                        onClick = { viewModel.cancelPairing() },
                                        isPrimary = false
                                    )
                                }
                            }
                        } else {
                            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                TvSettingsButton(
                                    label = stringResource(R.string.settings_pairing_action_start),
                                    onClick = {
                                        if (isAdminAllowed) {
                                            viewModel.startPairing()
                                        } else {
                                            showPinDialog = true
                                        }
                                    },
                                    isPrimary = true
                                )

                                TvSettingsButton(
                                    label = if (state.authToken.isNullOrBlank()) stringResource(R.string.settings_token_action_enter) else stringResource(R.string.settings_token_action_edit),
                                    onClick = {
                                        if (isAdminAllowed) {
                                            tokenEditValue = state.authToken.orEmpty()
                                            showTokenDialog = true
                                        } else {
                                            showPinDialog = true
                                        }
                                    },
                                    isPrimary = false
                                )

                                if (!state.authToken.isNullOrBlank() && isAdminAllowed) {
                                    TvSettingsButton(
                                        label = stringResource(R.string.settings_token_action_delete),
                                        onClick = { viewModel.saveToken(null) },
                                        isPrimary = false
                                    )
                                }
                            }
                        }

                        if (state.message != null) {
                            Spacer(modifier = Modifier.height(8.dp))
                            Text(
                                text = formatSettingsMessage(state.message.orEmpty()),
                                style = MaterialTheme.typography.bodySmall,
                                color = Color(0xFF34D399)
                            )
                        }
                    }
                }
            }

            // Section 3: Streaming & Audio Settings
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_streaming))
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                        // Audio Mode
                        Column {
                            Text(
                                text = stringResource(R.string.settings_audio_mode),
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.Bold,
                                color = colorResource(R.color.color_text_primary)
                            )
                            Text(
                                text = stringResource(R.string.settings_audio_mode_desc),
                                style = MaterialTheme.typography.bodySmall,
                                color = colorResource(R.color.color_text_secondary)
                            )
                            Spacer(modifier = Modifier.height(8.dp))
                            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                                TvSettingsOptionButton(
                                    label = stringResource(R.string.settings_audio_stereo),
                                    isSelected = state.audioMode == "stereo",
                                    onClick = { viewModel.saveAudioMode("stereo") }
                                )
                                TvSettingsOptionButton(
                                    label = stringResource(R.string.settings_audio_surround),
                                    isSelected = state.audioMode == "surround",
                                    onClick = { viewModel.saveAudioMode("surround") }
                                )
                            }
                        }

                        Spacer(modifier = Modifier.height(2.dp))

                        // DVR Mode / Timeshift
                        Column {
                            Text(
                                text = stringResource(R.string.settings_dvr_mode),
                                style = MaterialTheme.typography.titleSmall,
                                fontWeight = FontWeight.Bold,
                                color = colorResource(R.color.color_text_primary)
                            )
                            Text(
                                text = stringResource(R.string.settings_dvr_mode_desc),
                                style = MaterialTheme.typography.bodySmall,
                                color = colorResource(R.color.color_text_secondary)
                            )
                            Spacer(modifier = Modifier.height(8.dp))
                            Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                                TvSettingsOptionButton(
                                    label = stringResource(R.string.settings_dvr_live_only),
                                    isSelected = state.dvrMode == "live_only",
                                    onClick = { viewModel.saveDvrMode("live_only") }
                                )
                                TvSettingsOptionButton(
                                    label = stringResource(R.string.settings_dvr_1h),
                                    isSelected = state.dvrMode == "1h",
                                    onClick = { viewModel.saveDvrMode("1h") }
                                )
                                TvSettingsOptionButton(
                                    label = stringResource(R.string.settings_dvr_2h),
                                    isSelected = state.dvrMode == "2h",
                                    onClick = { viewModel.saveDvrMode("2h") }
                                )
                                TvSettingsOptionButton(
                                    label = stringResource(R.string.settings_dvr_4h),
                                    isSelected = state.dvrMode == "4h",
                                    onClick = { viewModel.saveDvrMode("4h") }
                                )
                            }
                        }
                    }
                }
            }

            // Section 4: Channel Scan & EPG
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_scan))
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                        Row(
                            modifier = Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Column(modifier = Modifier.weight(1f)) {
                                Text(
                                    text = stringResource(R.string.settings_scan_title),
                                    style = MaterialTheme.typography.titleSmall,
                                    fontWeight = FontWeight.Bold,
                                    color = colorResource(R.color.color_text_primary)
                                )
                                Text(
                                    text = stringResource(R.string.settings_scan_desc),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = colorResource(R.color.color_text_secondary)
                                )
                            }

                            Spacer(modifier = Modifier.width(16.dp))

                            val isScanRunning = state.scanState == "running" || state.isScanTriggering
                            TvSettingsButton(
                                label = if (isScanRunning) stringResource(R.string.settings_scan_running) else stringResource(R.string.settings_scan_start),
                                onClick = {
                                    if (isAdminAllowed) {
                                        viewModel.triggerChannelScan()
                                    } else {
                                        showPinDialog = true
                                    }
                                },
                                isPrimary = true
                            )
                        }

                        if (state.scanError != null) {
                            Text(
                                text = formatScanError(state.scanError.orEmpty()),
                                style = MaterialTheme.typography.bodySmall,
                                color = Color(0xFFEF4444)
                            )
                        }

                        // Progress Indicator
                        val progress = if (state.totalChannels > 0) {
                            state.scannedChannels.toFloat() / state.totalChannels.toFloat()
                        } else 0f

                        Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                            LinearProgressIndicator(
                                progress = { progress.coerceIn(0f, 1f) },
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .height(6.dp)
                                    .clip(RoundedCornerShape(3.dp)),
                                color = colorResource(R.color.color_action),
                                trackColor = Color(0xFF1E293B)
                            )
                            Row(
                                modifier = Modifier.fillMaxWidth(),
                                horizontalArrangement = Arrangement.SpaceBetween
                            ) {
                                Text(
                                    text = stringResource(R.string.settings_scan_progress, state.scannedChannels, state.totalChannels),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = colorResource(R.color.color_text_secondary)
                                )
                                Text(
                                    text = stringResource(R.string.settings_scan_status, state.updatedCount, state.scanState.uppercase()),
                                    style = MaterialTheme.typography.bodySmall,
                                    fontWeight = FontWeight.Bold,
                                    color = if (state.scanState == "running") Color(0xFF38BDF8) else Color(0xFF34D399)
                                )
                            }
                        }
                    }
                }
            }

            // Section 5: Diagnostics & System Status
            item {
                SettingsSectionTitle(title = stringResource(R.string.settings_section_diagnostics))
                Spacer(modifier = Modifier.height(6.dp))
                SettingsCard {
                    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        DiagnosticRow(label = stringResource(R.string.settings_diag_receiver), value = formatReceiverStatus(state.receiverStatus))
                        DiagnosticRow(label = stringResource(R.string.settings_diag_epg), value = formatEpgStatus(state.epgStatus))
                        DiagnosticRow(label = stringResource(R.string.settings_diag_app_engine), value = state.appVersion)

                        Spacer(modifier = Modifier.height(4.dp))

                        TvSettingsButton(
                            label = stringResource(R.string.settings_diag_refresh),
                            onClick = {
                                viewModel.refreshHealth()
                                viewModel.refreshScanStatus()
                                viewModel.refreshUnlockStatus()
                            },
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

    // Modal Dialog for Household PIN Unlock
    if (showPinDialog) {
        AlertDialog(
            onDismissRequest = { showPinDialog = false },
            title = {
                Text(
                    text = stringResource(R.string.settings_dialog_pin_title),
                    fontWeight = FontWeight.Bold,
                    color = Color.White
                )
            },
            text = {
                Column {
                    Text(
                        text = stringResource(R.string.settings_dialog_pin_desc),
                        style = MaterialTheme.typography.bodyMedium,
                        color = Color(0xFFCBD5E1)
                    )
                    Spacer(modifier = Modifier.height(12.dp))
                    OutlinedTextField(
                        value = pinEditValue,
                        onValueChange = { pinEditValue = it },
                        modifier = Modifier.fillMaxWidth(),
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
                        colors = OutlinedTextFieldDefaults.colors(
                            focusedBorderColor = colorResource(R.color.color_action),
                            unfocusedBorderColor = colorResource(R.color.color_border_subtle),
                            focusedTextColor = Color.White,
                            unfocusedTextColor = Color.White
                        )
                    )
                    if (state.unlockError != null) {
                        Spacer(modifier = Modifier.height(6.dp))
                        Text(
                            text = formatUnlockError(state.unlockError.orEmpty()),
                            style = MaterialTheme.typography.bodySmall,
                            color = Color(0xFFEF4444)
                        )
                    }
                }
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        if (pinEditValue.isNotBlank()) {
                            viewModel.unlockWithPin(pinEditValue)
                            showPinDialog = false
                        }
                    }
                ) {
                    Text(stringResource(R.string.settings_action_unlock), color = colorResource(R.color.color_action), fontWeight = FontWeight.Bold)
                }
            },
            dismissButton = {
                TextButton(onClick = { showPinDialog = false }) {
                    Text(stringResource(R.string.server_setup_cancel), color = Color.White)
                }
            },
            containerColor = colorResource(R.color.color_bg_elevated),
            shape = RoundedCornerShape(16.dp)
        )
    }

    // Modal Dialog for Token Input
    if (showTokenDialog) {
        AlertDialog(
            onDismissRequest = { showTokenDialog = false },
            title = {
                Text(
                    text = stringResource(R.string.settings_dialog_token_title),
                    fontWeight = FontWeight.Bold,
                    color = Color.White
                )
            },
            text = {
                Column {
                    Text(
                        text = stringResource(R.string.settings_dialog_token_desc),
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
                    Text(stringResource(R.string.settings_action_save), color = colorResource(R.color.color_action), fontWeight = FontWeight.Bold)
                }
            },
            dismissButton = {
                TextButton(onClick = { showTokenDialog = false }) {
                    Text(stringResource(R.string.server_setup_cancel), color = Color.White)
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

@Composable
private fun TvSettingsOptionButton(
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
        color = if (isSelected) colorResource(R.color.color_action) else Color(0xFF0F172A),
        border = BorderStroke(
            width = if (isFocused) 2.dp else 1.dp,
            color = if (isFocused) Color.White else if (isSelected) colorResource(R.color.color_action) else colorResource(R.color.color_border_subtle)
        )
    ) {
        Text(
            text = (if (isSelected) "✓ " else "") + label,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = if (isSelected) FontWeight.Bold else FontWeight.Medium,
            color = Color.White,
            modifier = Modifier.padding(horizontal = 14.dp, vertical = 6.dp)
        )
    }
}

@Composable
private fun formatReceiverStatus(status: String): String =
    io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy.formatReceiverStatus(status, androidx.compose.ui.platform.LocalContext.current)

@Composable
private fun formatEpgStatus(status: String): String =
    io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy.formatEpgStatus(status, androidx.compose.ui.platform.LocalContext.current)

@Composable
private fun formatSettingsMessage(raw: String): String =
    io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy.formatSettingsMessage(raw, androidx.compose.ui.platform.LocalContext.current)

@Composable
private fun formatPairingError(raw: String): String =
    io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy.formatPairingError(raw, androidx.compose.ui.platform.LocalContext.current)

@Composable
private fun formatUnlockError(raw: String): String =
    io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy.formatUnlockError(raw, androidx.compose.ui.platform.LocalContext.current)

@Composable
private fun formatScanError(raw: String): String =
    io.github.manugh.xg2g.android.ui.ErrorPresentationPolicy.formatScanError(raw, androidx.compose.ui.platform.LocalContext.current)
