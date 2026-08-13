package io.github.manugh.xg2g.android

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import org.json.JSONArray
import org.json.JSONObject

internal data class PersistedDeviceAuthState(
    val serverUrl: String,
    val deviceGrantId: String,
    val deviceGrant: String,
    val accessSessionId: String? = null,
    val accessToken: String? = null,
    val accessTokenExpiresAtEpochMs: Long? = null,
    val policyVersion: String? = null,
    val publishedEndpoints: List<PublishedEndpoint> = emptyList()
) {
    fun hasUsableAccessToken(nowEpochMs: Long): Boolean {
        val token = accessToken?.trim().takeIf { !it.isNullOrEmpty() } ?: return false
        val expiresAt = accessTokenExpiresAtEpochMs ?: return false
        return token.isNotEmpty() && nowEpochMs + ACCESS_TOKEN_EXPIRY_SKEW_MS < expiresAt
    }

    fun clearedAccessToken(): PersistedDeviceAuthState = copy(
        accessSessionId = null,
        accessToken = null,
        accessTokenExpiresAtEpochMs = null
    )

    fun matchesServerUrl(normalizedServerUrl: String): Boolean {
        if (serverUrl == normalizedServerUrl) {
            return true
        }
        // Normalize the persisted serverUrl so that old state saved with an
        // explicit default port (e.g. https://host:443/ui/) still matches the
        // stripped canonical form (https://host/ui/). Without this, upgrade
        // silently orphans all existing device auth and forces re-authentication.
        val normalizedStored = ServerTargetResolver.normalizeServerUrl(serverUrl)
        if (normalizedStored == normalizedServerUrl) {
            return true
        }
        return matchesPublishedEndpointServerUrl(normalizedServerUrl, publishedEndpoints)
    }

    private companion object {
        const val ACCESS_TOKEN_EXPIRY_SKEW_MS = 30_000L
    }
}

internal data class DeviceAuthLaunchCredentials(
    val deviceGrantId: String? = null,
    val deviceGrant: String? = null,
    val accessToken: String? = null,
    val accessTokenExpiresAtEpochMs: Long? = null
) {
    fun hasPersistableGrant(): Boolean {
        return !deviceGrantId.isNullOrBlank() && !deviceGrant.isNullOrBlank()
    }
}

internal interface PersistedDeviceAuthStateStore {
    fun load(): PersistedDeviceAuthState?
    fun save(state: PersistedDeviceAuthState)
    fun clear()
}

internal class DeviceAuthStore(
    context: Context,
    private val prefs: SharedPreferences = createEncryptedSharedPreferences(context)
) : PersistedDeviceAuthStateStore {



    override fun load(): PersistedDeviceAuthState? {
        val serverUrl = prefs.getString(PREF_SERVER_URL, null)?.trim()?.takeIf { it.isNotEmpty() }
            ?: return null
        val deviceGrantId = prefs.getString(PREF_DEVICE_GRANT_ID, null)?.trim()?.takeIf { it.isNotEmpty() }
        val deviceGrant = prefs.getString(PREF_DEVICE_GRANT, null)?.trim()?.takeIf { it.isNotEmpty() }
        if (deviceGrantId == null || deviceGrant == null) {
            clear()
            return null
        }

        return PersistedDeviceAuthState(
            serverUrl = serverUrl,
            deviceGrantId = deviceGrantId,
            deviceGrant = deviceGrant,
            accessSessionId = prefs.getString(PREF_ACCESS_SESSION_ID, null)
                ?.trim()
                ?.takeIf { it.isNotEmpty() },
            accessToken = prefs.getString(PREF_ACCESS_TOKEN, null)
                ?.trim()
                ?.takeIf { it.isNotEmpty() },
            accessTokenExpiresAtEpochMs = prefs.takeIf { it.contains(PREF_ACCESS_TOKEN_EXPIRES_AT_MS) }
                ?.getLong(PREF_ACCESS_TOKEN_EXPIRES_AT_MS, 0L)
                ?.takeIf { it > 0L },
            policyVersion = prefs.getString(PREF_POLICY_VERSION, null)
                ?.trim()
                ?.takeIf { it.isNotEmpty() },
            publishedEndpoints = decodePublishedEndpoints(prefs.getString(PREF_PUBLISHED_ENDPOINTS, null))
        )
    }

    override fun save(state: PersistedDeviceAuthState) {
        val normalizedServerUrl = ServerTargetResolver.normalizeServerUrl(state.serverUrl)
            ?: throw IllegalArgumentException("Invalid xg2g server URL for device auth state: ${state.serverUrl}")
        val deviceGrantId = state.deviceGrantId.trim()
        val deviceGrant = state.deviceGrant.trim()
        require(deviceGrantId.isNotEmpty()) { "deviceGrantId must not be empty" }
        require(deviceGrant.isNotEmpty()) { "deviceGrant must not be empty" }
        val publishedEndpoints = normalizePublishedEndpoints(state.publishedEndpoints)

        val editor = prefs.edit()
            .putString(PREF_SERVER_URL, normalizedServerUrl)
            .putString(PREF_DEVICE_GRANT_ID, deviceGrantId)
            .putString(PREF_DEVICE_GRANT, deviceGrant)

        state.accessSessionId
            ?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { editor.putString(PREF_ACCESS_SESSION_ID, it) }
            ?: editor.remove(PREF_ACCESS_SESSION_ID)
        state.accessToken
            ?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { editor.putString(PREF_ACCESS_TOKEN, it) }
            ?: editor.remove(PREF_ACCESS_TOKEN)
        state.accessTokenExpiresAtEpochMs
            ?.takeIf { it > 0L }
            ?.let { editor.putLong(PREF_ACCESS_TOKEN_EXPIRES_AT_MS, it) }
            ?: editor.remove(PREF_ACCESS_TOKEN_EXPIRES_AT_MS)
        state.policyVersion
            ?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { editor.putString(PREF_POLICY_VERSION, it) }
            ?: editor.remove(PREF_POLICY_VERSION)
        if (publishedEndpoints.isNotEmpty()) {
            editor.putString(PREF_PUBLISHED_ENDPOINTS, encodePublishedEndpoints(publishedEndpoints))
        } else {
            editor.remove(PREF_PUBLISHED_ENDPOINTS)
        }

        if (!editor.commit()) {
            throw IllegalStateException("Could not persist Android device auth state")
        }
    }

    override fun clear() {
        if (!prefs.edit().clear().commit()) {
            throw IllegalStateException("Could not clear Android device auth state")
        }
    }

    companion object {
        const val PREFS_NAME = "device_auth_store"
        const val LEGACY_PREFS_NAME = "device_auth_store"
        const val ENCRYPTED_PREFS_NAME = "xg2g_device_auth_encrypted"
        private const val TAG = "DeviceAuthStoreMigration"

        const val PREF_SERVER_URL = "server_url"
        const val PREF_DEVICE_GRANT_ID = "device_grant_id"
        const val PREF_DEVICE_GRANT = "device_grant"
        const val PREF_ACCESS_SESSION_ID = "access_session_id"
        const val PREF_ACCESS_TOKEN = "access_token"
        const val PREF_ACCESS_TOKEN_EXPIRES_AT_MS = "access_token_expires_at_ms"
        const val PREF_POLICY_VERSION = "policy_version"
        const val PREF_PUBLISHED_ENDPOINTS = "published_endpoints"

        fun migrateLegacyStoreIfNeeded(
            encryptedPrefs: SharedPreferences,
            legacyPrefs: SharedPreferences
        ): Boolean {
            if (encryptedPrefs === legacyPrefs) {
                return false
            }

            if (!legacyPrefs.contains(PREF_DEVICE_GRANT_ID)) {
                return false
            }

            val legacyGrantId = legacyPrefs.getString(PREF_DEVICE_GRANT_ID, null)?.trim()
            val legacyGrant = legacyPrefs.getString(PREF_DEVICE_GRANT, null)?.trim()
            val legacyServerUrl = legacyPrefs.getString(PREF_SERVER_URL, null)?.trim()

            if (legacyGrantId.isNullOrEmpty() || legacyGrant.isNullOrEmpty()) {
                legacyPrefs.edit().clear().commit()
                return false
            }

            val legacyAccessSession = legacyPrefs.getString(PREF_ACCESS_SESSION_ID, null)?.trim()
            val legacyAccessToken = legacyPrefs.getString(PREF_ACCESS_TOKEN, null)?.trim()
            val legacyExpiresAt = legacyPrefs.getLong(PREF_ACCESS_TOKEN_EXPIRES_AT_MS, 0L)
            val legacyPolicyVersion = legacyPrefs.getString(PREF_POLICY_VERSION, null)?.trim()
            val legacyEndpoints = legacyPrefs.getString(PREF_PUBLISHED_ENDPOINTS, null)

            return try {
                val editor = encryptedPrefs.edit()
                    .putString(PREF_SERVER_URL, legacyServerUrl)
                    .putString(PREF_DEVICE_GRANT_ID, legacyGrantId)
                    .putString(PREF_DEVICE_GRANT, legacyGrant)

                if (!legacyAccessSession.isNullOrEmpty()) editor.putString(PREF_ACCESS_SESSION_ID, legacyAccessSession)
                if (!legacyAccessToken.isNullOrEmpty()) editor.putString(PREF_ACCESS_TOKEN, legacyAccessToken)
                if (legacyExpiresAt > 0L) editor.putLong(PREF_ACCESS_TOKEN_EXPIRES_AT_MS, legacyExpiresAt)
                if (!legacyPolicyVersion.isNullOrEmpty()) editor.putString(PREF_POLICY_VERSION, legacyPolicyVersion)
                if (!legacyEndpoints.isNullOrEmpty()) editor.putString(PREF_PUBLISHED_ENDPOINTS, legacyEndpoints)

                val writeSuccess = editor.commit()
                if (!writeSuccess) {
                    logWarn(TAG, "EncryptedSharedPreferences commit failed during migration. Retaining legacy store.")
                    return false
                }

                val verifiedGrantId = encryptedPrefs.getString(PREF_DEVICE_GRANT_ID, null)?.trim()
                val verifiedGrant = encryptedPrefs.getString(PREF_DEVICE_GRANT, null)?.trim()
                if (verifiedGrantId != legacyGrantId || verifiedGrant != legacyGrant) {
                    logError(TAG, "EncryptedSharedPreferences verification failed after write. Retaining legacy store.")
                    return false
                }

                val clearSuccess = legacyPrefs.edit().clear().commit()
                if (!clearSuccess) {
                    logWarn(TAG, "Failed to clear legacy SharedPreferences after successful encrypted migration.")
                }
                logInfo(TAG, "Successfully migrated legacy plain-text device credentials to EncryptedSharedPreferences.")
                true
            } catch (e: Exception) {
                logError(TAG, "Exception during secret store migration: ${e.localizedMessage}. Retaining legacy store.", e)
                false
            }
        }

        private fun logInfo(tag: String, msg: String) {
            runCatching { Log.i(tag, msg) }
        }

        private fun logWarn(tag: String, msg: String) {
            runCatching { Log.w(tag, msg) }
        }

        private fun logError(tag: String, msg: String, t: Throwable? = null) {
            runCatching { Log.e(tag, msg, t) }
        }

        fun createEncryptedSharedPreferences(context: Context): SharedPreferences {
            val encryptedPrefs = try {
                val masterKey = MasterKey.Builder(context)
                    .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
                    .build()
                EncryptedSharedPreferences.create(
                    context,
                    ENCRYPTED_PREFS_NAME,
                    masterKey,
                    EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
                    EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
                )
            } catch (e: Exception) {
                logWarn(TAG, "EncryptedSharedPreferences creation failed (${e.localizedMessage}). Falling back to standard SharedPreferences.")
                context.getSharedPreferences(LEGACY_PREFS_NAME, Context.MODE_PRIVATE)
            }

            try {
                val legacyPrefs = context.getSharedPreferences(LEGACY_PREFS_NAME, Context.MODE_PRIVATE)
                migrateLegacyStoreIfNeeded(encryptedPrefs, legacyPrefs)
            } catch (e: Exception) {
                logError(TAG, "Failed to execute legacy SharedPreferences migration check: ${e.localizedMessage}", e)
            }

            return encryptedPrefs
        }

        fun encodePublishedEndpoints(values: List<PublishedEndpoint>): String {
            val normalized = normalizePublishedEndpoints(values)
            val array = JSONArray()
            normalized.forEach { endpoint ->
                array.put(
                    JSONObject()
                        .put("url", endpoint.url)
                        .put("kind", endpoint.kind)
                        .put("priority", endpoint.priority)
                        .put("tlsMode", endpoint.tlsMode)
                        .put("allowPairing", endpoint.allowPairing)
                        .put("allowStreaming", endpoint.allowStreaming)
                        .put("allowWeb", endpoint.allowWeb)
                        .put("allowNative", endpoint.allowNative)
                        .put("advertiseReason", endpoint.advertiseReason)
                        .put("source", endpoint.source)
                )
            }
            return array.toString()
        }

        fun decodePublishedEndpoints(raw: String?): List<PublishedEndpoint> {
            val trimmed = raw?.trim()?.takeIf { it.isNotEmpty() } ?: return emptyList()
            return runCatching {
                val array = JSONArray(trimmed)
                buildList {
                    for (index in 0 until array.length()) {
                        val item = array.optJSONObject(index) ?: continue
                        add(
                            PublishedEndpoint(
                                url = item.optString("url"),
                                kind = item.optString("kind"),
                                priority = item.optInt("priority"),
                                tlsMode = item.optString("tlsMode"),
                                allowPairing = item.optBoolean("allowPairing"),
                                allowStreaming = item.optBoolean("allowStreaming"),
                                allowWeb = item.optBoolean("allowWeb"),
                                allowNative = item.optBoolean("allowNative"),
                                advertiseReason = item.optString("advertiseReason"),
                                source = item.optString("source")
                            )
                        )
                    }
                }
            }.getOrDefault(emptyList()).let(::normalizePublishedEndpoints)
        }
    }
}
