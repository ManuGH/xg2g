package io.github.manugh.xg2g.android

import android.content.Context
import android.content.SharedPreferences
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
    private val prefs: SharedPreferences =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
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

    private companion object {
        const val PREFS_NAME = "device_auth_store"
        const val PREF_SERVER_URL = "server_url"
        const val PREF_DEVICE_GRANT_ID = "device_grant_id"
        const val PREF_DEVICE_GRANT = "device_grant"
        const val PREF_ACCESS_SESSION_ID = "access_session_id"
        const val PREF_ACCESS_TOKEN = "access_token"
        const val PREF_ACCESS_TOKEN_EXPIRES_AT_MS = "access_token_expires_at_ms"
        const val PREF_POLICY_VERSION = "policy_version"
        const val PREF_PUBLISHED_ENDPOINTS = "published_endpoints"

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
