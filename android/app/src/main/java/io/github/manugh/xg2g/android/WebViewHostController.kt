package io.github.manugh.xg2g.android

import android.annotation.SuppressLint
import android.content.ComponentCallbacks2
import android.net.Uri
import android.net.http.SslError
import android.os.SystemClock
import android.util.Log
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
import androidx.core.content.ContextCompat
import androidx.core.net.toUri
import androidx.core.view.isVisible
import androidx.lifecycle.Lifecycle
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import androidx.webkit.WebViewRenderProcess
import androidx.webkit.WebViewRenderProcessClient
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest

internal class WebViewHostController(
    private val activity: AppCompatActivity,
    initialWebView: WebView,
    private val rootContainer: FrameLayout,
    fullscreenContainer: FrameLayout,
    private val isTvDevice: Boolean,
    private val appVersionName: String,
    private val serializedHostCapabilities: String,
    private val serializedPlaybackCapabilities: String,
    private val callbacks: Callbacks
) {
    interface Callbacks {
        fun currentBaseUrl(): String?
        fun lastRequestedUrl(): String
        fun updateLastRequestedUrl(url: String)
        fun onMainFrameVisible()
        fun onMainFrameError(title: String, detail: String)
        fun onPlaybackActiveChanged(active: Boolean)
        fun startNativePlayback(request: NativePlaybackRequest)
        fun stopNativePlayback()
        fun currentNativePlaybackStateJson(): String
        fun isSetupVisible(): Boolean
        fun isErrorVisible(): Boolean
        fun openExternal(uri: Uri)
    }

    private var webView: WebView = initialWebView
    private var pendingRendererRecoveryUrl: String? = null
    private var lastTrimMemoryLevel: Int? = null
    private var rendererUnresponsiveAtMs: Long? = null
    private val recoveryPolicy = WebViewRecoveryPolicy()
    private val fullscreenController = WebViewFullscreenController(
        activity = activity,
        rootContainer = rootContainer,
        fullscreenContainer = fullscreenContainer,
        activeWebView = { webView },
        shouldShowWebView = { !callbacks.isSetupVisible() && !callbacks.isErrorVisible() }
    )
    private val hostBridge = HostJavascriptBridge(
        activity = activity,
        serializedHostCapabilities = serializedHostCapabilities,
        serializedPlaybackCapabilities = serializedPlaybackCapabilities,
        activeWebView = { webView },
        callbacks = object : HostJavascriptBridge.Callbacks {
            override fun onPlaybackActiveChanged(active: Boolean) {
                callbacks.onPlaybackActiveChanged(active)
            }

            override fun shouldRequestInputFocus(): Boolean {
                return !callbacks.isSetupVisible() && !callbacks.isErrorVisible()
            }

            override fun startNativePlayback(request: NativePlaybackRequest) {
                callbacks.startNativePlayback(request)
            }

            override fun stopNativePlayback() {
                callbacks.stopNativePlayback()
            }

            override fun currentNativePlaybackStateJson(): String {
                return callbacks.currentNativePlaybackStateJson()
            }
        }
    )

    val activeWebView: WebView
        get() = webView

    init {
        configureWebView(webView)
    }

    fun onResume() {
        webView.onResume()
        webView.resumeTimers()

        if (!callbacks.isSetupVisible() && !callbacks.isErrorVisible()) {
            pendingRendererRecoveryUrl?.let { recoveryUrl ->
                pendingRendererRecoveryUrl = null
                Log.i(TAG, "Reloading deferred WebView state after background recovery")
                loadUrl(recoveryUrl)
                return
            }
        }

        hostBridge.publishCurrentNativePlaybackState()
        hostBridge.requestInputFocus()
    }

    fun onPause() {
        webView.onPause()
        webView.pauseTimers()
    }

    @Suppress("DEPRECATION")
    fun onTrimMemory(level: Int) {
        lastTrimMemoryLevel = level

        if (shouldReleaseWebViewUnderTrimMemory(level)) {
            val recoveryUrl = currentRecoveryUrl() ?: return
            Log.w(
                TAG,
                "Releasing background WebView under trim memory level=${recoveryPolicy.describeTrimMemoryLevel(level)} recoveryUrl=${recoveryPolicy.describeUrlForLog(recoveryUrl)}"
            )
            pendingRendererRecoveryUrl = recoveryUrl
            replaceWebView()
            return
        }

        if (level >= ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW) {
            Log.w(
                TAG,
                "Trim memory signal level=${recoveryPolicy.describeTrimMemoryLevel(level)} visible=${webView.isVisible} foreground=${!shouldDeferRendererRecovery()} url=${recoveryPolicy.describeUrlForLog(currentRecoveryUrl())}"
            )
        }
    }

    @Suppress("DEPRECATION")
    fun onLowMemory() {
        onTrimMemory(ComponentCallbacks2.TRIM_MEMORY_COMPLETE)
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

    fun hasCustomView(): Boolean = fullscreenController.hasCustomView

    fun hideCustomView() {
        fullscreenController.hideCustomView(notifyCallback = true)
    }

    fun dispatchHostMediaKey(action: String) {
        hostBridge.dispatchMediaKey(action, callbacks.lastRequestedUrl())
    }

    fun requestInputFocus() {
        hostBridge.requestInputFocus()
    }

    fun publishNativePlaybackState(stateJson: String) {
        hostBridge.publishNativePlaybackState(stateJson)
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
        configureRendererProcessClient(target)

        target.webChromeClient = object : WebChromeClient() {
            override fun onShowCustomView(view: View?, callback: CustomViewCallback?) {
                fullscreenController.showCustomView(view, callback)
            }

            override fun onHideCustomView() {
                fullscreenController.hideCustomView(notifyCallback = true)
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
                callbacks.onMainFrameVisible()
                fullscreenController.onPageCommitVisible()
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
                showMainFrameError(
                    view = view,
                    title = activity.getString(R.string.webview_error_title),
                    detail = describeMainFrameError(error.errorCode, error.description?.toString())
                )
            }

            override fun onReceivedSslError(view: WebView, handler: SslErrorHandler, error: SslError) {
                handler.cancel()
                showMainFrameError(
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
                showMainFrameError(
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

                val recoveryUrl = currentRecoveryUrl()
                val deferRecovery = shouldDeferRendererRecovery()

                Log.w(
                    TAG,
                    "WebView renderer exited; didCrash=${detail.didCrash()} foreground=${!deferRecovery} recoveryUrl=${recoveryPolicy.describeUrlForLog(recoveryUrl)}"
                )
                rendererUnresponsiveAtMs = null
                replaceWebView()
                if (recoveryUrl != null && deferRecovery) {
                    pendingRendererRecoveryUrl = recoveryUrl
                    return true
                }

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

    private fun configureRendererProcessClient(target: WebView) {
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_VIEW_RENDERER_CLIENT_BASIC_USAGE)) {
            return
        }

        WebViewCompat.setWebViewRenderProcessClient(
            target,
            ContextCompat.getMainExecutor(activity),
            object : WebViewRenderProcessClient() {
                override fun onRenderProcessUnresponsive(
                    view: WebView,
                    renderer: WebViewRenderProcess?
                ) {
                    if (view !== webView) {
                        return
                    }

                    rendererUnresponsiveAtMs = SystemClock.elapsedRealtime()
                    val recoveryUrl = currentRecoveryUrl()
                    val backgrounded = shouldDeferRendererRecovery()
                    Log.w(
                        TAG,
                        "WebView renderer unresponsive; foreground=${!backgrounded} trim=${recoveryPolicy.describeTrimMemoryLevel(lastTrimMemoryLevel)} url=${recoveryPolicy.describeUrlForLog(recoveryUrl)}"
                    )

                    if (backgrounded && recoveryUrl != null && maybeTerminateRenderer(renderer)) {
                        pendingRendererRecoveryUrl = recoveryUrl
                        Log.w(
                            TAG,
                            "Requested renderer termination while backgrounded to preserve deferred recovery"
                        )
                    }
                }

                override fun onRenderProcessResponsive(
                    view: WebView,
                    renderer: WebViewRenderProcess?
                ) {
                    if (view !== webView) {
                        return
                    }

                    val unresponsiveAt = rendererUnresponsiveAtMs
                    rendererUnresponsiveAtMs = null
                    val durationMs = if (unresponsiveAt != null) {
                        SystemClock.elapsedRealtime() - unresponsiveAt
                    } else {
                        null
                    }
                    Log.i(
                        TAG,
                        "WebView renderer responsive again after=${durationMs ?: "unknown"}ms url=${recoveryPolicy.describeUrlForLog(currentRecoveryUrl())}"
                    )
                }
            }
        )
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

    private fun showMainFrameError(view: WebView, title: String, detail: String) {
        webView.isVisible = false
        view.stopLoading()
        callbacks.onMainFrameError(title = title, detail = detail)
    }

    private fun replaceWebView() {
        rendererUnresponsiveAtMs = null
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
        if (target === webView) {
            fullscreenController.hideCustomView(notifyCallback = false)
        }
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

    private fun shouldDeferRendererRecovery(): Boolean {
        return recoveryPolicy.shouldDeferRendererRecovery(
            isFinishing = activity.isFinishing,
            isDestroyed = activity.isDestroyed,
            isStarted = activity.lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED),
            hasWindowFocus = activity.window.decorView.hasWindowFocus()
        )
    }

    private fun shouldReleaseWebViewUnderTrimMemory(level: Int): Boolean {
        return recoveryPolicy.shouldReleaseWebViewUnderTrimMemory(
            trimMemoryLevel = level,
            isSetupVisible = callbacks.isSetupVisible(),
            isErrorVisible = callbacks.isErrorVisible(),
            pendingRendererRecoveryUrl = pendingRendererRecoveryUrl,
            shouldDeferRendererRecovery = shouldDeferRendererRecovery(),
            recoveryUrl = currentRecoveryUrl()
        )
    }

    private fun currentRecoveryUrl(): String? {
        return recoveryPolicy.recoveryUrl(
            lastRequestedUrl = callbacks.lastRequestedUrl(),
            currentBaseUrl = callbacks.currentBaseUrl()
        )
    }

    private fun maybeTerminateRenderer(renderer: WebViewRenderProcess?): Boolean {
        if (renderer == null) {
            return false
        }
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_VIEW_RENDERER_TERMINATE)) {
            return false
        }

        return runCatching { renderer.terminate() }.getOrDefault(false)
    }

    private companion object {
        private const val ABOUT_BLANK = "about:blank"
        private const val TAG = "Xg2gWebViewHost"
    }
}
