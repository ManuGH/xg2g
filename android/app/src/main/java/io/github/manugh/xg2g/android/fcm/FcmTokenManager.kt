package io.github.manugh.xg2g.android.fcm

import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.apiV3Url
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.auth.createNativeAuthenticatedOkHttpClient
import io.github.manugh.xg2g.android.playback.net.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

internal class FcmTokenManager(
    stateStore: PersistedDeviceAuthStateStore,
    dpopProvider: DPoPProvider,
    stateMachine: io.github.manugh.xg2g.android.auth.AuthStateMachine? = null,
    private val okHttpClient: OkHttpClient = createNativeAuthenticatedOkHttpClient(
        stateStore = stateStore,
        dpopProvider = dpopProvider,
        stateMachine = stateMachine
    )
) {
    suspend fun registerFcmToken(
        baseUrl: String,
        fcmToken: String,
        bearerToken: String? = null
    ): Boolean = withContext(Dispatchers.IO) {
        val parsed = baseUrl.toHttpUrlOrNull() ?: return@withContext false
        val url = apiV3Url(parsed, "notifications", "push-subscriptions")

        val json = JSONObject().apply {
            put("endpoint", fcmToken)
            put("channel", "fcm")
            put("keys", JSONObject().apply {
                put("p256dh", "")
                put("auth", "")
            })
        }

        val request = Request.Builder()
            .url(url)
            .post(json.toString().toRequestBody(JSON_MEDIA_TYPE))
            .build()
            .withSameOriginHeaders(url)

        val response = okHttpClient.newCall(request).execute()
        response.isSuccessful
    }

    companion object {
        private val JSON_MEDIA_TYPE = "application/json; charset=utf-8".toMediaType()
    }
}
