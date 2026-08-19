package io.github.manugh.xg2g.android.auth

import io.github.manugh.xg2g.android.RefreshedDeviceSession
import io.github.manugh.xg2g.android.apiV3Url
import io.github.manugh.xg2g.android.contract.DeviceGrantResponse
import io.github.manugh.xg2g.android.contract.DeviceRefreshRequest
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/**
 * The one place that speaks POST /api/v3/auth/device/refresh.
 *
 * Both device-auth transports need it and both used to carry their own copy,
 * which is how one of them kept posting to /auth/device/session — a route the
 * server had already removed — long after the other was migrated.
 */
internal fun buildDeviceRefreshRequest(
    uiBaseUrl: HttpUrl,
    refreshToken: String,
    dpopProvider: DPoPProvider
): Request {
    val refreshUrl = apiV3Url(uiBaseUrl, "auth", "device", "refresh")
    val body = DeviceRefreshRequest(refreshToken = refreshToken)
        .toJson()
        .toString()
        .toRequestBody(JSON_MEDIA_TYPE)

    return Request.Builder()
        .url(refreshUrl)
        .post(body)
        // The proof is bound to this exact URL and carries no `ath`: the refresh
        // is authenticated by the token in the body plus possession of the
        // device key, not by an access token.
        .header("DPoP", dpopProvider.createProof("POST", refreshUrl.toString()))
        .build()
        .withSameOriginHeaders(uiBaseUrl)
}

/** Decodes a refresh response through the generated contract type. */
internal fun refreshedSessionFrom(json: JSONObject): RefreshedDeviceSession {
    val grant = DeviceGrantResponse.fromJson(json)
    return RefreshedDeviceSession(
        deviceId = grant.deviceId,
        accessToken = grant.accessToken,
        rotatedRefreshToken = grant.refreshToken,
        expiresInSeconds = grant.expiresIn,
        scope = grant.scope
    )
}

private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
