package io.github.manugh.xg2g.android

import android.annotation.SuppressLint
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.webkit.CookieManager
import android.webkit.SslErrorHandler
import android.webkit.SslError
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.view.View
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import com.google.android.material.button.MaterialButton

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView
    private lateinit var errorContainer: View
    private lateinit var errorTitle: TextView
    private lateinit var errorDetail: TextView
    private lateinit var retryButton: MaterialButton
    private lateinit var openInBrowserButton: MaterialButton
    private var lastRequestedUrl: String = BuildConfig.DEFAULT_BASE_URL

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        WebView.setWebContentsDebuggingEnabled(BuildConfig.WEBVIEW_DEBUGGING)

        setContentView(R.layout.activity_main)
        webView = findViewById(R.id.webview)
        errorContainer = findViewById(R.id.error_container)
        errorTitle = findViewById(R.id.error_title)
        errorDetail = findViewById(R.id.error_detail)
        retryButton = findViewById(R.id.retry_button)
        openInBrowserButton = findViewById(R.id.open_in_browser_button)
        configureWebView()
        configureErrorUi()
        installBackHandler()

        if (savedInstanceState == null) {
            loadAppUrl(resolveStartUrl())
        } else {
            lastRequestedUrl = savedInstanceState.getString(STATE_LAST_REQUESTED_URL) ?: resolveStartUrl()
            webView.restoreState(savedInstanceState)
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        loadAppUrl(resolveStartUrl())
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putString(STATE_LAST_REQUESTED_URL, lastRequestedUrl)
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
            setSupportMultipleWindows(false)
            userAgentString = "${userAgentString} xg2g-android/${BuildConfig.VERSION_NAME}"
        }

        webView.webChromeClient = WebChromeClient()
        webView.webViewClient = object : WebViewClient() {
            override fun onPageCommitVisible(view: WebView, url: String?) {
                hideErrorUi()
            }

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

            override fun onReceivedError(
                view: WebView,
                request: WebResourceRequest,
                error: WebResourceError
            ) {
                if (!request.isForMainFrame) {
                    return
                }

                showErrorUi(
                    title = getString(R.string.webview_error_title),
                    detail = describeMainFrameError(error.errorCode, error.description?.toString()),
                )
            }

            override fun onReceivedSslError(
                view: WebView,
                handler: SslErrorHandler,
                error: SslError
            ) {
                handler.cancel()
                showErrorUi(
                    title = getString(R.string.webview_ssl_error_title),
                    detail = getString(R.string.webview_ssl_error_detail, Uri.parse(lastRequestedUrl).host ?: lastRequestedUrl),
                )
            }
        }
    }

    private fun configureErrorUi() {
        retryButton.setOnClickListener {
            loadAppUrl(lastRequestedUrl)
        }
        openInBrowserButton.setOnClickListener {
            openExternal(Uri.parse(lastRequestedUrl))
        }
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (errorContainer.visibility == View.VISIBLE) {
                    loadAppUrl(lastRequestedUrl)
                    return
                }
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

    private fun loadAppUrl(url: String) {
        lastRequestedUrl = url
        hideErrorUi()
        webView.loadUrl(url)
    }

    private fun showErrorUi(title: String, detail: String) {
        errorTitle.text = title
        errorDetail.text = detail
        errorContainer.visibility = View.VISIBLE
        webView.visibility = View.GONE
    }

    private fun hideErrorUi() {
        errorContainer.visibility = View.GONE
        webView.visibility = View.VISIBLE
    }

    private fun describeMainFrameError(errorCode: Int, description: String?): String {
        return when (errorCode) {
            WebViewClient.ERROR_HOST_LOOKUP -> getString(R.string.webview_error_host_lookup)
            WebViewClient.ERROR_CONNECT -> getString(R.string.webview_error_connect)
            WebViewClient.ERROR_TIMEOUT -> getString(R.string.webview_error_timeout)
            WebViewClient.ERROR_TOO_MANY_REQUESTS -> getString(R.string.webview_error_too_many_requests)
            else -> description?.takeIf { it.isNotBlank() } ?: getString(R.string.webview_error_generic)
        }
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
        private const val STATE_LAST_REQUESTED_URL = "state_last_requested_url"
    }
}
