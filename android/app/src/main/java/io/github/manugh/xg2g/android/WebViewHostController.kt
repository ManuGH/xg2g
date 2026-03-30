package io.github.manugh.xg2g.android

import android.annotation.SuppressLint
import android.net.Uri
import android.net.http.SslError
import android.view.View
import android.view.ViewGroup
import android.webkit.CookieManager
import android.webkit.RenderProcessGoneDetail
import android.webkit.SslErrorHandler
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebSettings
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import androidx.appcompat.app.AppCompatActivity
import androidx.core.net.toUri
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.core.view.isVisible

internal class WebViewHostController(
    private val activity: AppCompatActivity,
    initialWebView: WebView,
    private val rootContainer: FrameLayout,
    private val fullscreenContainer: FrameLayout,
    private val isTvDevice: Boolean,
    private val appVersionName: String,
    private val hostCapabilitiesJson: String,
    private val callbacks: Callbacks
) {
    interface Callbacks {
        fun currentBaseUrl(): String?
        fun lastRequestedUrl(): String
        fun updateLastRequestedUrl(url: String)
        fun onPageReady()
        fun onMainFrameError(title: String, detail: String)
        fun onPlaybackActiveChanged(active: Boolean)
        fun isSetupVisible(): Boolean
        fun isErrorVisible(): Boolean
        fun openExternal(uri: Uri)
    }

    private var webView: WebView = initialWebView
    private var customView: View? = null
    private var customViewCallback: WebChromeClient.CustomViewCallback? = null
    private val hostBridge = HostJavascriptBridge(
        activity = activity,
        hostCapabilitiesJson = hostCapabilitiesJson,
        currentWebView = { webView },
        callbacks = object : HostJavascriptBridge.Callbacks {
            override fun onPlaybackActiveChanged(active: Boolean) {
                callbacks.onPlaybackActiveChanged(active)
            }

            override fun shouldRequestInputFocus(): Boolean {
                return !callbacks.isSetupVisible() && !callbacks.isErrorVisible()
            }
        }
    )

    val currentWebView: WebView
        get() = webView

    init {
        configureWebView(webView)
    }

    fun onResume() {
        webView.onResume()
        webView.resumeTimers()
        hostBridge.requestInputFocus()
    }

    fun onPause() {
        webView.onPause()
        webView.pauseTimers()
    }

    fun saveState(outState: android.os.Bundle) {
        webView.saveState(outState)
    }

    fun restoreState(savedInstanceState: android.os.Bundle): android.webkit.WebBackForwardList? {
        return webView.restoreState(savedInstanceState)
    }

    fun loadUrl(url: String) {
        callbacks.onPlaybackActiveChanged(false)
        callbacks.updateLastRequestedUrl(url)
        webView.loadUrl(url)
    }

    fun canGoBack(): Boolean = webView.canGoBack()

    fun goBack() {
        webView.goBack()
    }

    fun hasCustomView(): Boolean = customView != null

    fun hideCustomView() {
        hideCustomViewInternal(notifyCallback = true)
    }

    fun dispatchHostMediaKey(action: String) {
        hostBridge.dispatchMediaKey(action, callbacks.lastRequestedUrl())
    }

    fun release(renderProcessGone: Boolean = false) {
        releaseWebView(webView, renderProcessGone = renderProcessGone)
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun configureWebView(target: WebView) {
        CookieManager.getInstance().apply {
            setAcceptCookie(true)
            setAcceptThirdPartyCookies(target, true)
        }

        target.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            mediaPlaybackRequiresUserGesture = false
            mixedContentMode = WebSettings.MIXED_CONTENT_NEVER_ALLOW
            allowFileAccess = false
            allowContentAccess = false
            safeBrowsingEnabled = true
            builtInZoomControls = false
            displayZoomControls = false
            setSupportMultipleWindows(false)
            setSupportZoom(false)
            userAgentString = "$userAgentString xg2g-android/$appVersionName (${if (isTvDevice) "AndroidTV" else "Android"})"
        }

        target.isFocusable = true
        target.isFocusableInTouchMode = true
        hostBridge.attach(target)

        target.webChromeClient = object : WebChromeClient() {
            override fun onShowCustomView(view: View?, callback: CustomViewCallback?) {
                if (view == null) {
                    callback?.onCustomViewHidden()
                    return
                }
                if (customView != null) {
                    hideCustomViewInternal(notifyCallback = true)
                }
                (view.parent as? ViewGroup)?.removeView(view)
                customView = view
                customViewCallback = callback
                fullscreenContainer.removeAllViews()
                fullscreenContainer.addView(
                    view,
                    FrameLayout.LayoutParams(
                        ViewGroup.LayoutParams.MATCH_PARENT,
                        ViewGroup.LayoutParams.MATCH_PARENT
                    )
                )
                fullscreenContainer.isVisible = true
                webView.isVisible = false
                enterFullscreen()
            }

            override fun onHideCustomView() {
                hideCustomViewInternal(notifyCallback = true)
            }
        }

        target.webViewClient = object : WebViewClient() {
            override fun onPageStarted(view: WebView, url: String?, favicon: android.graphics.Bitmap?) {
                if (!url.isNullOrBlank() && url != ABOUT_BLANK) {
                    callbacks.updateLastRequestedUrl(url)
                }
            }

            override fun onPageCommitVisible(view: WebView, url: String?) {
                if (view !== webView || url == ABOUT_BLANK) {
                    return
                }
                if (!url.isNullOrBlank()) {
                    callbacks.updateLastRequestedUrl(url)
                }
                hostBridge.publishEnvironment(view)
                callbacks.onPageReady()
                if (customView == null) {
                    webView.isVisible = true
                }
                hostBridge.requestInputFocus()
            }

            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest
            ): Boolean {
                if (!request.isForMainFrame) return false
                val targetUri = request.url
                val baseUrl = callbacks.currentBaseUrl()

                return when {
                    targetUri.scheme !in setOf("http", "https") -> {
                        callbacks.openExternal(targetUri)
                        true
                    }
                    baseUrl != null &&
                        !ServerTargetResolver.isSameOrigin(targetUri.toString(), baseUrl) -> {
                        callbacks.openExternal(targetUri)
                        true
                    }
                    else -> {
                        callbacks.updateLastRequestedUrl(targetUri.toString())
                        false
                    }
                }
            }

            override fun onReceivedError(view: WebView, request: WebResourceRequest, error: WebResourceError) {
                if (!request.isForMainFrame) return
                showWebViewError(
                    view = view,
                    title = activity.getString(R.string.webview_error_title),
                    detail = describeMainFrameError(error.errorCode, error.description?.toString())
                )
            }

            override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
                handler.cancel()
                showWebViewError(
                    view = view,
                    title = activity.getString(R.string.webview_ssl_error_title),
                    detail = activity.getString(
                        R.string.webview_ssl_error_detail,
                        callbacks.lastRequestedUrl().toUri().host ?: callbacks.lastRequestedUrl()
                    )
                )
            }

            override fun onReceivedHttpError(
                view: WebView,
                request: WebResourceRequest,
                errorResponse: WebResourceResponse
            ) {
                if (!request.isForMainFrame || errorResponse.statusCode < 400) return
                showWebViewError(
                    view = view,
                    title = activity.getString(R.string.webview_error_title),
                    detail = activity.getString(
                        R.string.webview_http_error_detail,
                        errorResponse.statusCode
                    )
                )
            }

            override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                if (view !== webView) {
                    releaseWebView(view, renderProcessGone = true)
                    return true
                }

                replaceWebViewAfterCrash()
                callbacks.onMainFrameError(
                    title = activity.getString(R.string.webview_error_title),
                    detail = if (detail.didCrash()) {
                        activity.getString(R.string.webview_renderer_crashed)
                    } else {
                        activity.getString(R.string.webview_renderer_gone)
                    }
                )
                return true
            }
        }
    }

    private fun describeMainFrameError(errorCode: Int, description: String?): String {
        return when (errorCode) {
            WebViewClient.ERROR_HOST_LOOKUP -> activity.getString(R.string.webview_error_host_lookup)
            WebViewClient.ERROR_CONNECT -> activity.getString(R.string.webview_error_connect)
            WebViewClient.ERROR_TIMEOUT -> activity.getString(R.string.webview_error_timeout)
            WebViewClient.ERROR_TOO_MANY_REQUESTS -> activity.getString(R.string.webview_error_too_many_requests)
            else -> description?.takeIf { it.isNotBlank() }
                ?: activity.getString(R.string.webview_error_generic)
        }
    }

    private fun showWebViewError(view: WebView, title: String, detail: String) {
        webView.isVisible = false
        view.stopLoading()
        callbacks.onMainFrameError(title = title, detail = detail)
    }

    private fun hideCustomViewInternal(notifyCallback: Boolean) {
        val activeCustomView = customView ?: return
        fullscreenContainer.removeView(activeCustomView)
        fullscreenContainer.isVisible = false
        webView.isVisible = !callbacks.isSetupVisible() && !callbacks.isErrorVisible()
        customView = null
        val callback = customViewCallback
        customViewCallback = null
        if (notifyCallback) {
            callback?.onCustomViewHidden()
        }
        exitFullscreen()
    }

    private fun replaceWebViewAfterCrash() {
        val crashedWebView = webView
        val layoutParams = crashedWebView.layoutParams
        releaseWebView(crashedWebView, renderProcessGone = true)

        val replacement = WebView(activity).apply {
            id = R.id.webview
            this.layoutParams = layoutParams
            isVisible = false
        }
        rootContainer.addView(replacement, 0)
        webView = replacement
        configureWebView(replacement)
    }

    private fun releaseWebView(target: WebView, renderProcessGone: Boolean) {
        callbacks.onPlaybackActiveChanged(false)
        hideCustomViewInternal(notifyCallback = false)
        runCatching { target.stopLoading() }
        if (!renderProcessGone) {
            runCatching { target.loadUrl(ABOUT_BLANK) }
        }
        runCatching { target.onPause() }
        hostBridge.detach(target)
        runCatching { target.removeAllViews() }
        target.webChromeClient = WebChromeClient()
        target.webViewClient = WebViewClient()
        (target.parent as? ViewGroup)?.removeView(target)
        runCatching { target.destroy() }
    }

    private fun enterFullscreen() {
        WindowInsetsControllerCompat(activity.window, rootContainer).apply {
            hide(WindowInsetsCompat.Type.systemBars())
            systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
    }

    private fun exitFullscreen() {
        WindowInsetsControllerCompat(activity.window, rootContainer)
            .show(WindowInsetsCompat.Type.systemBars())
    }

    private companion object {
        private const val ABOUT_BLANK = "about:blank"
    }
}
