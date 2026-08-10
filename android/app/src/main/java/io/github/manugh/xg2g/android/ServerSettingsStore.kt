package io.github.manugh.xg2g.android

import android.content.Context
import android.content.SharedPreferences
import androidx.core.content.edit

internal class ServerSettingsStore(
    context: Context,
    private val prefs: SharedPreferences =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
) {
    fun getServerUrl(): String? {
        val rawUrl = prefs.getString(PREF_SERVER_URL, null) ?: return null
        val normalizedUrl = ServerTargetResolver.normalizeServerUrl(rawUrl) ?: return null
        if (normalizedUrl != rawUrl) {
            saveServerUrl(normalizedUrl)
        }
        return normalizedUrl
    }

    fun saveServerUrl(url: String) {
        prefs.edit { putString(PREF_SERVER_URL, url) }
    }

    fun getAuthToken(): String? {
        return prefs.getString(PREF_AUTH_TOKEN, null)?.trim()?.takeIf { it.isNotEmpty() }
    }

    fun saveAuthToken(token: String?) {
        val cleaned = token?.trim()?.takeIf { it.isNotEmpty() }
        prefs.edit {
            if (cleaned != null) {
                putString(PREF_AUTH_TOKEN, cleaned)
            } else {
                remove(PREF_AUTH_TOKEN)
            }
        }
    }

    private companion object {
        private const val PREFS_NAME = "app_settings"
        private const val PREF_SERVER_URL = "server_url"
        private const val PREF_AUTH_TOKEN = "auth_token"
    }
}
