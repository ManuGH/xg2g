package io.github.manugh.xg2g.android

import java.net.URI
import java.net.URLDecoder
import java.nio.charset.StandardCharsets

internal object ServerTargetResolver {
    const val EXTRA_BASE_URL = "base_url"
    const val EXTRA_AUTH_TOKEN = "auth_token"
    const val EXTRA_DEVICE_GRANT_ID = "device_grant_id"
    const val EXTRA_DEVICE_GRANT = "device_grant"
    const val EXTRA_ACCESS_TOKEN = "access_token"
    const val EXTRA_ACCESS_TOKEN_EXPIRES_AT = "access_token_expires_at"

    private const val CUSTOM_SCHEME = "xg2g"
    private const val QUERY_BASE_URL = "base_url"
    private const val QUERY_AUTH_TOKEN = "auth_token"
    private const val QUERY_DEVICE_GRANT_ID = "device_grant_id"
    private const val QUERY_DEVICE_GRANT = "device_grant"
    private const val QUERY_ACCESS_TOKEN = "access_token"
    private const val QUERY_ACCESS_TOKEN_EXPIRES_AT = "access_token_expires_at"
    private const val UI_BASE_SEGMENT = "/ui"

    fun resolveConfiguredBaseUrl(
        existingBaseUrl: String?,
        overrideUrl: String?,
        deepLinkUrl: String?
    ): String? {
        normalizeServerUrl(overrideUrl.orEmpty())?.let { return it }

        parseUri(deepLinkUrl)?.let { deepLinkUri ->
            when (deepLinkUri.scheme?.lowercase()) {
                CUSTOM_SCHEME -> {
                    val linkedBaseUrl = queryParameter(deepLinkUri.rawQuery, QUERY_BASE_URL).orEmpty()
                    normalizeServerUrl(linkedBaseUrl)?.let { return it }
                }
                "http", "https" -> {
                    normalizeServerUrlFromDeepLink(deepLinkUri)?.let { return it }
                }
            }
        }

        return existingBaseUrl?.let(::normalizeServerUrl)
    }

    fun resolveStartUrl(
        baseUrl: String,
        overrideUrl: String?,
        deepLinkUrl: String?
    ): String {
        val normalizedBaseUrl = normalizeServerUrl(baseUrl) ?: baseUrl
        val deepLink = deepLinkUrl

        if (deepLink != null &&
            isSameOrigin(deepLink, normalizedBaseUrl) &&
            isUnderBasePath(deepLink, normalizedBaseUrl)
        ) {
            return deepLink
        }

        return normalizeServerUrl(overrideUrl.orEmpty()) ?: normalizedBaseUrl
    }

    fun resolveAuthToken(
        overrideToken: String?,
        deepLinkUrl: String?
    ): String? {
        overrideToken?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { return it }

        val deepLinkUri = parseUri(deepLinkUrl) ?: return null
        if (!deepLinkUri.scheme.equals(CUSTOM_SCHEME, ignoreCase = true)) {
            return null
        }

        return queryParameter(deepLinkUri.rawQuery, QUERY_AUTH_TOKEN)
            ?.trim()
            ?.takeIf { it.isNotEmpty() }
    }

    fun resolveAccessToken(
        overrideToken: String?,
        deepLinkUrl: String?
    ): String? {
        overrideToken?.trim()
            ?.takeIf { it.isNotEmpty() }
            ?.let { return it }

        val deepLinkUri = parseUri(deepLinkUrl) ?: return null
        if (!deepLinkUri.scheme.equals(CUSTOM_SCHEME, ignoreCase = true)) {
            return null
        }

        return queryParameter(deepLinkUri.rawQuery, QUERY_ACCESS_TOKEN)
            ?.trim()
            ?.takeIf { it.isNotEmpty() }
    }

    fun resolveDeviceAuthLaunchCredentials(
        overrideDeviceGrantId: String?,
        overrideDeviceGrant: String?,
        overrideAccessToken: String?,
        overrideAccessTokenExpiresAt: String?,
        deepLinkUrl: String?
    ): DeviceAuthLaunchCredentials? {
        val deepLinkUri = parseUri(deepLinkUrl)
        val deviceGrantId = overrideDeviceGrantId?.trim()?.takeIf { it.isNotEmpty() }
            ?: queryParameter(deepLinkUri?.rawQuery, QUERY_DEVICE_GRANT_ID)
                ?.trim()
                ?.takeIf { it.isNotEmpty() }
        val deviceGrant = overrideDeviceGrant?.trim()?.takeIf { it.isNotEmpty() }
            ?: queryParameter(deepLinkUri?.rawQuery, QUERY_DEVICE_GRANT)
                ?.trim()
                ?.takeIf { it.isNotEmpty() }
        val accessToken = resolveAccessToken(overrideAccessToken, deepLinkUrl)
        val accessTokenExpiresAtEpochMs = parseDeviceAuthExpiryEpochMs(
            overrideAccessTokenExpiresAt?.trim()?.takeIf { it.isNotEmpty() }
                ?: queryParameter(deepLinkUri?.rawQuery, QUERY_ACCESS_TOKEN_EXPIRES_AT)
        )

        if (deviceGrantId == null &&
            deviceGrant == null &&
            accessToken == null &&
            accessTokenExpiresAtEpochMs == null
        ) {
            return null
        }

        return DeviceAuthLaunchCredentials(
            deviceGrantId = deviceGrantId,
            deviceGrant = deviceGrant,
            accessToken = accessToken,
            accessTokenExpiresAtEpochMs = accessTokenExpiresAtEpochMs
        )
    }

    fun normalizeServerUrl(input: String): String? {
        val rawInput = input.trim()
        if (rawInput.isEmpty()) return null

        val withScheme = if (
            !rawInput.startsWith("http://", ignoreCase = true) &&
            !rawInput.startsWith("https://", ignoreCase = true)
        ) {
            "https://$rawInput"
        } else {
            rawInput
        }

        val uri = parseUri(withScheme) ?: return null
        val scheme = uri.scheme?.lowercase() ?: return null
        if (scheme !in setOf("http", "https")) return null

        val host = uri.host ?: return null
        val authority = buildString {
            append(host)
            if (uri.port != -1) {
                append(':')
                append(uri.port)
            }
        }

        return URI(
            scheme,
            authority,
            normalizeBasePath(uri.rawPath.orEmpty()),
            null,
            null
        ).toString()
    }

    fun isSameOrigin(targetUrl: String, baseUrl: String): Boolean {
        val target = parseUri(targetUrl) ?: return false
        val base = parseUri(baseUrl) ?: return false

        return target.scheme.equals(base.scheme, ignoreCase = true) &&
            target.host.equals(base.host, ignoreCase = true) &&
            effectivePort(target) == effectivePort(base)
    }

    fun isUnderBasePath(targetUrl: String, baseUrl: String): Boolean {
        val target = parseUri(targetUrl) ?: return false
        val base = parseUri(baseUrl) ?: return false

        val basePath = normalizeBasePath(base.rawPath.orEmpty())
        val targetPath = target.rawPath ?: return false
        return targetPath == basePath.removeSuffix("/") || targetPath.startsWith(basePath)
    }

    private fun normalizeServerUrlFromDeepLink(uri: URI): String? {
        val scheme = uri.scheme?.lowercase() ?: return null
        if (scheme !in setOf("http", "https")) return null

        val host = uri.host ?: return null
        val authority = buildString {
            append(host)
            if (uri.port != -1) {
                append(':')
                append(uri.port)
            }
        }

        return URI(
            scheme,
            authority,
            extractUiBasePath(uri.rawPath.orEmpty()),
            null,
            null
        ).toString()
    }

    private fun parseUri(value: String?): URI? {
        if (value.isNullOrBlank()) return null
        return runCatching { URI(value) }.getOrNull()
    }

    private fun normalizeBasePath(path: String): String {
        if (path.isBlank() || path == "/") {
            return "/ui/"
        }
        return if (path.endsWith("/")) path else "$path/"
    }

    private fun extractUiBasePath(path: String): String {
        if (path.isBlank() || path == "/") {
            return "/ui/"
        }

        val uiIndex = path.indexOf("/ui")
        if (uiIndex == -1) {
            return normalizeBasePath(path)
        }

        val endIndex = uiIndex + UI_BASE_SEGMENT.length
        return normalizeBasePath(path.substring(0, endIndex))
    }

    private fun queryParameter(rawQuery: String?, name: String): String? {
        if (rawQuery.isNullOrBlank()) return null

        return rawQuery
            .split('&')
            .asSequence()
            .mapNotNull { part ->
                val separatorIndex = part.indexOf('=')
                val rawKey = if (separatorIndex >= 0) part.substring(0, separatorIndex) else part
                val rawValue = if (separatorIndex >= 0) part.substring(separatorIndex + 1) else ""
                decode(rawKey) to decode(rawValue)
            }
            .firstOrNull { (key, _) -> key == name }
            ?.second
    }

    private fun decode(value: String): String {
        return URLDecoder.decode(value, StandardCharsets.UTF_8)
    }

    private fun effectivePort(uri: URI): Int {
        return when {
            uri.port != -1 -> uri.port
            uri.scheme.equals("https", ignoreCase = true) -> 443
            uri.scheme.equals("http", ignoreCase = true) -> 80
            else -> -1
        }
    }
}
