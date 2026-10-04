package io.github.manugh.xg2g.android.ui

import android.content.Context
import io.github.manugh.xg2g.android.R

internal object ErrorPresentationPolicy {

    /**
     * Resolves a settings message code into a catalog-backed user-facing string.
     * Raw diagnostic or error text NEVER reaches the returned string.
     */
    fun formatSettingsMessage(
        raw: String,
        getString: (Int) -> String,
        getStringWithArg: (Int, Any) -> String
    ): String {
        if (raw.isBlank()) return ""
        return when {
            raw == "pairing_success" || raw == "Gerät erfolgreich gekoppelt!" -> getString(R.string.settings_pairing_success)
            raw == "admin_unlocked" || raw == "Admin-Modus freigeschaltet." -> getString(R.string.settings_admin_unlocked_msg)
            raw == "admin_locked" || raw == "Admin-Modus gesperrt." -> getString(R.string.settings_admin_locked_msg)
            raw == "token_saved" || raw == "API-Token erfolgreich gespeichert." -> getString(R.string.settings_token_saved)
            raw.startsWith("profile_activated:") -> {
                val name = raw.removePrefix("profile_activated:")
                getStringWithArg(R.string.settings_profile_activated, name)
            }
            // Unknown or raw text is strictly replaced by a generic safe catalog message
            else -> getString(R.string.settings_message_generic)
        }
    }

    fun formatSettingsMessage(raw: String, context: Context): String =
        formatSettingsMessage(raw, { context.getString(it) }, { resId, arg -> context.getString(resId, arg) })

    /**
     * Resolves pairing error codes into catalog-backed strings.
     * Raw exceptions are suppressed and replaced by catalog fallbacks.
     */
    fun formatPairingError(raw: String, getString: (Int) -> String): String {
        return when {
            raw == "pairing_expired" || raw.contains("abgelaufen") -> getString(R.string.settings_pairing_error_expired)
            else -> getString(R.string.settings_pairing_error_start)
        }
    }

    fun formatPairingError(raw: String, context: Context): String =
        formatPairingError(raw) { context.getString(it) }

    /**
     * Resolves unlock error codes into catalog-backed strings.
     */
    fun formatUnlockError(raw: String, getString: (Int) -> String): String {
        return when {
            raw == "wrong_pin" || raw.contains("Falscher Haushalt-PIN") -> getString(R.string.settings_admin_wrong_pin)
            else -> getString(R.string.settings_admin_unlock_failed)
        }
    }

    fun formatUnlockError(raw: String, context: Context): String =
        formatUnlockError(raw) { context.getString(it) }

    /**
     * Resolves scan error codes into catalog-backed strings.
     */
    fun formatScanError(raw: String, getString: (Int) -> String): String {
        return when {
            else -> getString(R.string.settings_scan_start_failed)
        }
    }

    fun formatScanError(raw: String, context: Context): String =
        formatScanError(raw) { context.getString(it) }

    /**
     * Resolves receiver status into catalog-backed strings.
     */
    fun formatReceiverStatus(status: String, getString: (Int) -> String): String {
        return when (status.lowercase()) {
            "ready", "online", "online (bereit)" -> getString(R.string.settings_diag_status_online_ready)
            "offline" -> getString(R.string.settings_diag_status_offline)
            "unreachable", "nicht erreichbar" -> getString(R.string.settings_diag_status_unreachable)
            "loading", "lade..." -> getString(R.string.settings_diag_status_loading)
            else -> getString(R.string.settings_diag_status_unavailable)
        }
    }

    fun formatReceiverStatus(status: String, context: Context): String =
        formatReceiverStatus(status) { context.getString(it) }

    /**
     * Resolves EPG status into catalog-backed strings.
     */
    fun formatEpgStatus(status: String, getString: (Int) -> String): String {
        return when (status.lowercase()) {
            "active", "aktiv (synchronisiert)" -> getString(R.string.settings_diag_status_epg_active)
            "limited", "eingeschränkt" -> getString(R.string.settings_diag_status_epg_limited)
            "unavailable", "nicht verfügbar" -> getString(R.string.settings_diag_status_unavailable)
            "loading", "lade..." -> getString(R.string.settings_diag_status_loading)
            else -> getString(R.string.settings_diag_status_unavailable)
        }
    }

    fun formatEpgStatus(status: String, context: Context): String =
        formatEpgStatus(status) { context.getString(it) }

    /**
     * Resolves web preparation errors into a catalog-backed user-visible detail message.
     * Raw throwable message is suppressed.
     */
    fun resolveWebErrorDetail(getString: (Int) -> String): String {
        return getString(R.string.webview_error_generic)
    }

    fun resolveWebErrorDetail(context: Context): String =
        resolveWebErrorDetail { context.getString(it) }

    /**
     * Resolves auth revoked error into catalog-backed user-visible detail message.
     */
    fun resolveAuthRevokedDetail(getString: (Int) -> String): String {
        return getString(R.string.auth_revoked_detail)
    }

    fun resolveAuthRevokedDetail(context: Context): String =
        resolveAuthRevokedDetail { context.getString(it) }

    /**
     * Resolves auth reauth error into catalog-backed user-visible detail message.
     */
    fun resolveAuthReauthDetail(getString: (Int) -> String): String {
        return getString(R.string.auth_reauth_detail)
    }

    fun resolveAuthReauthDetail(context: Context): String =
        resolveAuthReauthDetail { context.getString(it) }
}
