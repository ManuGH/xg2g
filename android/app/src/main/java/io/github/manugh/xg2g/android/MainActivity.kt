package io.github.manugh.xg2g.android

import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.webkit.CookieManager
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        WebView.setWebContentsDebuggingEnabled(BuildConfig.WEBVIEW_DEBUGGING)

        webView = WebView(this)
        setContentView(webView)
        configureWebView()
        installBackHandler()

        if (savedInstanceState == null) {
            webView.loadUrl(resolveStartUrl())
        } else {
            webView.restoreState(savedInstanceState)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        webView.loadUrl(resolveStartUrl())
    }

    override fun onSaveInstanceState(outState: Bundle) {
        webView.saveState(outState)
        super.onSaveInstanceState(outState)
    }

    override fun onDestroy() {
        webView.destroy()
        super.onDestroy()
    }

    @SuppressLint("SetJavaScriptEnabled")
    private fun configureWebView() {
        CookieManager.getInstance().apply {
            setAcceptCookie(true)
            setAcceptThirdPartyCookies(webView, true)
        }

        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            mediaPlaybackRequiresUserGesture = false
            allowFileAccess = false
            allowContentAccess = false
            builtInZoomControls = false
            displayZoomControls = false
            userAgentString = "${userAgentString} xg2g-android/${BuildConfig.VERSION_NAME}"
        }

        webView.webChromeClient = WebChromeClient()
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(
                view: WebView,
                request: WebResourceRequest
            ): Boolean {
                val target = request.url
                val baseUri = Uri.parse(BuildConfig.DEFAULT_BASE_URL)
                val baseHost = baseUri.host

                return when {
                    target.scheme !in setOf("http", "https") -> {
                        openExternal(target)
                        true
                    }
                    target.host != null && baseHost != null && target.host != baseHost -> {
                        openExternal(target)
                        true
                    }
                    else -> false
                }
            }
        }
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (webView.canGoBack()) {
                    webView.goBack()
                    return
                }
                isEnabled = false
                onBackPressedDispatcher.onBackPressed()
            }
        })
    }

    private fun resolveStartUrl(): String {
        val data = intent.dataString
        val baseUrl = BuildConfig.DEFAULT_BASE_URL
        val baseUri = Uri.parse(baseUrl)

        // Check if intent data matches the configured base URL (including scheme, host, and path prefix)
        if (data != null && baseUri.host != null && data.startsWith("${baseUri.scheme}://${baseUri.host}${baseUri.path}")) {
            return data
        }

        val overrideUrl = intent.getStringExtra(EXTRA_BASE_URL)?.trim().orEmpty()
        val raw = if (overrideUrl.isNotEmpty()) overrideUrl else baseUrl
        return if (raw.endsWith("/")) raw else "$raw/"
    }

    private fun openExternal(uri: Uri) {
        val intent = Intent(Intent.ACTION_VIEW, uri)
        try {
            startActivity(intent)
        } catch (_: ActivityNotFoundException) {
            // Ignore invalid external targets instead of crashing the host shell.
        }
    }

    companion object {
        const val EXTRA_BASE_URL = "base_url"
    }
}
