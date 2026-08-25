package io.github.manugh.xg2g.android.transport.playback

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.webkit.CookieManager
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * Loads the channel logo shown on the player surface.
 *
 * Resolution against the deployment address and the cookie the request needs
 * are both transport concerns; the activity only wants a bitmap.
 */
internal suspend fun loadPlaybackLogoBitmap(baseUrl: String?, requestedUrl: String): Bitmap? =
    withContext(Dispatchers.IO) {
        val absoluteUrl = requestedUrl.toHttpUrlOrNull()?.toString()
            ?: baseUrl?.toHttpUrlOrNull()?.resolve(requestedUrl)?.toString()
            ?: return@withContext null

        runCatching {
            val connection = URL(absoluteUrl).openConnection() as HttpURLConnection
            val cookies = CookieManager.getInstance().getCookie(absoluteUrl)
            if (!cookies.isNullOrBlank()) {
                connection.setRequestProperty("Cookie", cookies)
            }
            connection.connectTimeout = 5_000
            connection.readTimeout = 5_000
            connection.inputStream.use(BitmapFactory::decodeStream)
        }.getOrNull()
    }
