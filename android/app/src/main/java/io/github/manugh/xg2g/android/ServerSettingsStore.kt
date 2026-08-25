package io.github.manugh.xg2g.android


import android.content.Context
import android.content.SharedPreferences
import androidx.core.content.edit
import io.github.manugh.xg2g.android.profile.ProfileSelectionStore
import io.github.manugh.xg2g.android.transport.ServerTargetResolver

internal class ServerSettingsStore(
    context: Context,
    private val prefs: SharedPreferences =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
) : ProfileSelectionStore {
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

    fun getAudioMode(): String {
        return prefs.getString(PREF_AUDIO_MODE, "stereo") ?: "stereo"
    }

    fun saveAudioMode(mode: String) {
        prefs.edit { putString(PREF_AUDIO_MODE, mode) }
    }

    fun getDvrMode(): String {
        return prefs.getString(PREF_DVR_MODE, "2h") ?: "2h"
    }

    fun saveDvrMode(mode: String) {
        prefs.edit { putString(PREF_DVR_MODE, mode) }
    }

    override fun getSelectedProfileId(): String? {
        return prefs.getString(PREF_SELECTED_PROFILE_ID, null)?.trim()?.takeIf { it.isNotEmpty() }
    }

    override fun saveSelectedProfileId(profileId: String?) {
        val cleaned = profileId?.trim()?.takeIf { it.isNotEmpty() }
        prefs.edit {
            if (cleaned != null) {
                putString(PREF_SELECTED_PROFILE_ID, cleaned)
            } else {
                remove(PREF_SELECTED_PROFILE_ID)
            }
        }
    }

    private companion object {
        private const val PREFS_NAME = "app_settings"
        private const val PREF_SERVER_URL = "server_url"
        private const val PREF_AUTH_TOKEN = "auth_token"
        private const val PREF_AUDIO_MODE = "audio_mode"
        private const val PREF_DVR_MODE = "dvr_mode"
        private const val PREF_SELECTED_PROFILE_ID = "selected_profile_id"
    }
}
