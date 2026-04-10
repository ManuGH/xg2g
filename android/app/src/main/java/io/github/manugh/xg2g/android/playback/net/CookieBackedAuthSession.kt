package io.github.manugh.xg2g.android.playback.net

import android.util.Log
import android.webkit.CookieManager
import okhttp3.Headers
import okhttp3.HttpUrl
import okhttp3.Request

internal interface AuthCookieSession {
    fun hasSessionCookie(url: HttpUrl, cookieName: String): Boolean
    fun applyCookies(url: HttpUrl, builder: Request.Builder)
    fun storeCookies(url: HttpUrl, headers: Headers)
    fun cookieHeader(url: HttpUrl): String?
    fun clearSessionCookie(url: HttpUrl, cookieName: String, cookiePath: String = "/api/v3/")
}

internal class CookieBackedAuthSession(
    private val cookieManager: CookieManager = CookieManager.getInstance()
) : AuthCookieSession {
    override fun hasSessionCookie(url: HttpUrl, cookieName: String): Boolean =
        cookieManager.getCookie(url.toString())
            ?.split(';')
            ?.map(String::trim)
            ?.any { it.startsWith("$cookieName=") }
            ?: false

    override fun applyCookies(url: HttpUrl, builder: Request.Builder) {
        cookieHeader(url)
            ?.takeIf { it.isNotBlank() }
            ?.let { cookies ->
                Log.d(TAG, "applyCookies path=${url.encodedPath} cookieCount=${cookies.split(';').size}")
                builder.header("Cookie", cookies)
            }
    }

    override fun storeCookies(url: HttpUrl, headers: Headers) {
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

    override fun cookieHeader(url: HttpUrl): String? = cookieManager.getCookie(url.toString())

    override fun clearSessionCookie(url: HttpUrl, cookieName: String, cookiePath: String) {
        cookieManager.setCookie(
            url.toString(),
            "$cookieName=; Max-Age=0; Path=$cookiePath; HttpOnly"
        )
        cookieManager.flush()
        Log.d(TAG, "clearSessionCookie path=${url.encodedPath} cookieName=$cookieName cookiePath=$cookiePath")
    }

    private companion object {
        const val TAG = "Xg2gCookieSession"
    }
}
