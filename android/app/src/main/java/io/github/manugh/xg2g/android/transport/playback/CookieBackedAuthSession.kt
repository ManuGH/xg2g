package io.github.manugh.xg2g.android.transport.playback

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
    private val providedCookieManager: CookieManager? = null
) : AuthCookieSession {
    // CookieManager boots the full WebView provider on Fire TV. Delay that several-second cost
    // until a web or authenticated playback path actually needs browser cookies.
    private val cookieManager: CookieManager by lazy(LazyThreadSafetyMode.NONE) {
        providedCookieManager ?: CookieManager.getInstance()
    }

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
        val rootUrl = "${url.scheme}://${url.host}/"
        headers.values("Set-Cookie").forEach { value ->
            cookieManager.setCookie(url.toString(), value)
            if (value.contains("xg2g_session=", ignoreCase = true)) {
                val rootValue = value.replace(Regex("Path=[^;]+", RegexOption.IGNORE_CASE), "Path=/")
                cookieManager.setCookie(rootUrl, rootValue)
            }
        }
        cookieManager.flush()
        if (headers.values("Set-Cookie").isNotEmpty()) {
            Log.d(
                TAG,
                "storeCookies path=${url.encodedPath} setCookieCount=${headers.values("Set-Cookie").size} hasSessionCookie=${hasSessionCookie(url, "xg2g_session")}"
            )
        }
    }

    override fun cookieHeader(url: HttpUrl): String? {
        val exactMatch = cookieManager.getCookie(url.toString())
        if (!exactMatch.isNullOrBlank()) {
            return exactMatch
        }
        val rootUrl = "${url.scheme}://${url.host}/"
        return cookieManager.getCookie(rootUrl)
    }

    override fun clearSessionCookie(url: HttpUrl, cookieName: String, cookiePath: String) {
        sessionCookieDeletionPaths(cookiePath).forEach { path ->
            cookieManager.setCookie(
                url.toString(),
                "$cookieName=; Max-Age=0; Path=$path; HttpOnly"
            )
        }
        cookieManager.flush()
        Log.d(TAG, "clearSessionCookie path=${url.encodedPath} cookieName=$cookieName cookiePaths=${sessionCookieDeletionPaths(cookiePath)}")
    }

    private companion object {
        const val TAG = "Xg2gCookieSession"
    }
}

internal fun sessionCookieDeletionPaths(cookiePath: String): List<String> =
    listOf(cookiePath, "/").distinct()
