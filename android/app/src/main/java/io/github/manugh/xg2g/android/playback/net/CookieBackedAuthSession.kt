package io.github.manugh.xg2g.android.playback.net

import android.util.Log
import android.webkit.CookieManager
import okhttp3.Headers
import okhttp3.HttpUrl
import okhttp3.Request

internal class CookieBackedAuthSession(
    private val cookieManager: CookieManager = CookieManager.getInstance()
) {
    fun hasSessionCookie(url: HttpUrl, cookieName: String): Boolean =
        cookieManager.getCookie(url.toString())
            ?.split(';')
            ?.map(String::trim)
            ?.any { it.startsWith("$cookieName=") }
            ?: false

    fun applyCookies(url: HttpUrl, builder: Request.Builder) {
        cookieHeader(url)
            ?.takeIf { it.isNotBlank() }
            ?.let { cookies ->
                Log.d(TAG, "applyCookies path=${url.encodedPath} cookieCount=${cookies.split(';').size}")
                builder.header("Cookie", cookies)
            }
    }

    fun storeCookies(url: HttpUrl, headers: Headers) {
        headers.values("Set-Cookie").forEach { value ->
            cookieManager.setCookie(url.toString(), value)
        }
        cookieManager.flush()
        if (headers.values("Set-Cookie").isNotEmpty()) {
            Log.d(
                TAG,
                "storeCookies path=${url.encodedPath} setCookieCount=${headers.values("Set-Cookie").size} hasSessionCookie=${hasSessionCookie(url, "xg2g_session")}"
            )
        }
    }

    fun cookieHeader(url: HttpUrl): String? = cookieManager.getCookie(url.toString())

    private companion object {
        const val TAG = "Xg2gCookieSession"
    }
}
