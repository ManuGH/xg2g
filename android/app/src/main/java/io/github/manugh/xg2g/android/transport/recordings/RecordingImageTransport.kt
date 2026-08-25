package io.github.manugh.xg2g.android.transport.recordings

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import io.github.manugh.xg2g.android.DeviceAuthStore
import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.transport.auth.createNativeAuthenticatedOkHttpClient
import io.github.manugh.xg2g.android.transport.playback.withSameOriginHeaders
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.Request

/**
 * Fetches a recording thumbnail.
 *
 * The screen that shows the picture has no business holding an HTTP client, a
 * DPoP provider or a thumbnail URL. It asks for a bitmap; everything between
 * the request and the bytes is transport, and lives here.
 */
internal suspend fun loadRecordingThumbnail(
    context: Context,
    baseUrl: String,
    recordingId: String
): Bitmap? = withContext(Dispatchers.IO) {
    runCatching {
        val parsedBaseUrl = baseUrl.toHttpUrlOrNull() ?: return@runCatching null
        val thumbnailUrl = recordingThumbnailUrl(parsedBaseUrl, recordingId)
        val client = createNativeAuthenticatedOkHttpClient(
            DeviceAuthStore(context.applicationContext),
            AndroidKeystoreDPoPProvider()
        )
        val request = Request.Builder()
            .url(thumbnailUrl)
            .get()
            .build()
            .withSameOriginHeaders(thumbnailUrl)

        client.newCall(request).execute().use { response ->
            if (!response.isSuccessful) return@use null
            val bytes = response.body?.bytes()?.takeIf { it.isNotEmpty() } ?: return@use null
            BitmapFactory.decodeByteArray(bytes, 0, bytes.size)
        }
    }.getOrNull()
}
