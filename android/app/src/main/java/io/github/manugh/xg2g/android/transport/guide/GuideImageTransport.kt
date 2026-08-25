package io.github.manugh.xg2g.android.transport.guide

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.webkit.CookieManager
import java.net.HttpURLConnection
import java.net.URL
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * Turns a channel logo reference into an absolute URL against the deployment.
 *
 * Resolving a relative reference needs the deployment address, which is a
 * transport fact. The list view used to build this string itself, which is how
 * an absolute URL literal ended up in a Compose file.
 */
internal fun resolveGuideLogoUrl(baseUrl: String, logoUrl: String?): String? {
    val normalized = logoUrl?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    normalized.toHttpUrlOrNull()?.let { return it.toString() }
    val base = baseUrl.toHttpUrlOrNull() ?: return null
    return base.resolve(normalized)?.toString()
}

/** Loads a guide bitmap, carrying the WebView session cookie the picture needs. */
internal suspend fun loadGuideBitmap(url: String): Bitmap? = withContext(Dispatchers.IO) {
    runCatching {
        val connection = URL(url).openConnection() as HttpURLConnection
        val cookies = CookieManager.getInstance().getCookie(url)
        if (!cookies.isNullOrBlank()) {
            connection.setRequestProperty("Cookie", cookies)
        }
        connection.connectTimeout = 4_000
        connection.readTimeout = 4_000
        connection.inputStream.use(BitmapFactory::decodeStream)
    }.getOrNull()
}
