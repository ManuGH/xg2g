package io.github.manugh.xg2g.android

import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.net.Uri
import android.os.Bundle
import android.view.KeyEvent
import android.view.View
import android.view.WindowManager
import android.webkit.WebView
import android.widget.FrameLayout
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.core.net.toUri
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout
import org.json.JSONObject

class MainActivity : AppCompatActivity() {
    private lateinit var rootContainer: FrameLayout
    private lateinit var fullscreenContainer: FrameLayout
    private lateinit var webViewController: WebViewHostController

    private lateinit var setupContainer: View
    private lateinit var serverUrlLayout: TextInputLayout
    private lateinit var serverUrlEditText: TextInputEditText
    private lateinit var connectButton: MaterialButton
    private lateinit var cancelSetupButton: MaterialButton

    private lateinit var errorContainer: View
    private lateinit var errorTitle: TextView
    private lateinit var errorDetail: TextView
    private lateinit var retryButton: MaterialButton
    private lateinit var changeServerButton: MaterialButton
    private lateinit var openInBrowserButton: MaterialButton

    private var lastRequestedUrl: String = ""
    private var playbackActive = false

    private val serverSettings by lazy { ServerSettingsStore(this) }
    private val isTvDevice by lazy(LazyThreadSafetyMode.NONE) { detectTvDevice() }
    private val hostCapabilitiesJson by lazy(LazyThreadSafetyMode.NONE) { buildHostCapabilitiesJson() }
    private val webView: WebView
        get() = webViewController.currentWebView

    override fun onCreate(savedInstanceState: Bundle?) {
        installSplashScreen()
        super.onCreate(savedInstanceState)

        WebView.setWebContentsDebuggingEnabled(BuildConfig.WEBVIEW_DEBUGGING)

        setContentView(R.layout.activity_main)

        rootContainer = findViewById(R.id.root_container)
        fullscreenContainer = findViewById(R.id.fullscreen_container)
        val initialWebView: WebView = findViewById(R.id.webview)

        setupContainer = findViewById(R.id.setup_container)
        serverUrlLayout = findViewById(R.id.server_url_layout)
        serverUrlEditText = findViewById(R.id.server_url_edit_text)
        connectButton = findViewById(R.id.connect_button)
        cancelSetupButton = findViewById(R.id.cancel_setup_button)

        errorContainer = findViewById(R.id.error_container)
        errorTitle = findViewById(R.id.error_title)
        errorDetail = findViewById(R.id.error_detail)
        retryButton = findViewById(R.id.retry_button)
        changeServerButton = findViewById(R.id.change_server_button)
        openInBrowserButton = findViewById(R.id.open_in_browser_button)

        webViewController = WebViewHostController(
            activity = this,
            initialWebView = initialWebView,
            rootContainer = rootContainer,
            fullscreenContainer = fullscreenContainer,
            isTvDevice = isTvDevice,
            appVersionName = BuildConfig.VERSION_NAME,
            hostCapabilitiesJson = hostCapabilitiesJson,
            callbacks = object : WebViewHostController.Callbacks {
                override fun currentBaseUrl(): String? = serverSettings.getServerUrl()

                override fun lastRequestedUrl(): String = lastRequestedUrl

                override fun updateLastRequestedUrl(url: String) {
                    lastRequestedUrl = url
                }

                override fun onPageReady() {
                    hideErrorUi()
                    hideSetupUi()
                }

                override fun onMainFrameError(title: String, detail: String) {
                    showErrorUi(title, detail)
                }

                override fun onPlaybackActiveChanged(active: Boolean) {
                    setPlaybackActive(active)
                }

                override fun isSetupVisible(): Boolean = setupContainer.isVisible

                override fun isErrorVisible(): Boolean = errorContainer.isVisible

                override fun openExternal(uri: Uri) {
                    this@MainActivity.openExternal(uri)
                }
            }
        )

        configureSetupUi()
        configureErrorUi()
        installBackHandler()

        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = serverSettings.getServerUrl(),
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )
        if (configuredBaseUrl != null) {
            serverSettings.saveServerUrl(configuredBaseUrl)
            val startUrl = ServerTargetResolver.resolveStartUrl(
                baseUrl = configuredBaseUrl,
                overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
                deepLinkUrl = intent.dataString
            )
            lastRequestedUrl = startUrl
            if (savedInstanceState == null) {
                loadAppUrl(startUrl)
            } else {
                lastRequestedUrl = savedInstanceState.getString(STATE_LAST_REQUESTED_URL) ?: startUrl
                val restoredState = webViewController.restoreState(savedInstanceState)
                if (restoredState == null || webView.url.isNullOrBlank()) {
                    loadAppUrl(lastRequestedUrl)
                } else if (!webViewController.hasCustomView()) {
                    hideErrorUi()
                    hideSetupUi()
                    webView.isVisible = true
                }
            }
        } else {
            showSetupUi()
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = serverSettings.getServerUrl(),
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )
        if (configuredBaseUrl != null) {
            serverSettings.saveServerUrl(configuredBaseUrl)
            loadAppUrl(
                ServerTargetResolver.resolveStartUrl(
                    baseUrl = configuredBaseUrl,
                    overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
                    deepLinkUrl = intent.dataString
                )
            )
        }
    }

    override fun onResume() {
        super.onResume()
        webViewController.onResume()
        applyPlaybackKeepScreenOn(playbackActive)
    }

    override fun onPause() {
        applyPlaybackKeepScreenOn(false)
        webViewController.onPause()
        super.onPause()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putString(STATE_LAST_REQUESTED_URL, lastRequestedUrl)
        webViewController.saveState(outState)
        super.onSaveInstanceState(outState)
    }

    override fun onDestroy() {
        webViewController.release(renderProcessGone = false)
        super.onDestroy()
    }

    private fun configureSetupUi() {
        connectButton.setOnClickListener {
            val input = serverUrlEditText.text?.toString()?.trim().orEmpty()
            if (validateAndSaveUrl(input)) {
                loadAppUrl(serverSettings.getServerUrl()!!)
            }
        }
        cancelSetupButton.setOnClickListener {
            val savedUrl = serverSettings.getServerUrl()
            if (savedUrl != null) {
                loadAppUrl(savedUrl)
            }
        }
    }

    private fun validateAndSaveUrl(input: String): Boolean {
        val normalizedUrl = ServerTargetResolver.normalizeServerUrl(input)
        if (normalizedUrl == null) {
            serverUrlLayout.error = getString(R.string.server_setup_invalid_url)
            return false
        }
        serverUrlLayout.error = null
        serverSettings.saveServerUrl(normalizedUrl)
        return true
    }

    private fun configureErrorUi() {
        retryButton.setOnClickListener { loadAppUrl(lastRequestedUrl) }
        changeServerButton.setOnClickListener { showSetupUi() }
        openInBrowserButton.setOnClickListener { openExternal(lastRequestedUrl.toUri()) }
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (webViewController.hasCustomView()) {
                    webViewController.hideCustomView()
                    return
                }
                if (setupContainer.isVisible) {
                    val savedUrl = serverSettings.getServerUrl()
                    if (savedUrl != null) {
                        loadAppUrl(savedUrl)
                    } else {
                        isEnabled = false
                        onBackPressedDispatcher.onBackPressed()
                    }
                    return
                }
                if (errorContainer.isVisible) {
                    showSetupUi()
                    return
                }
                if (webViewController.canGoBack()) {
                    webViewController.goBack()
                    return
                }
                isEnabled = false
                onBackPressedDispatcher.onBackPressed()
            }
        })
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (keyCode == KeyEvent.KEYCODE_BACK) {
            onBackPressedDispatcher.onBackPressed()
            return true
        }
        if (event?.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0 && handleMediaKey(keyCode)) {
            return true
        }
        return super.onKeyDown(keyCode, event)
    }

    private fun loadAppUrl(url: String) {
        setPlaybackActive(false)
        lastRequestedUrl = url
        hideErrorUi()
        hideSetupUi()
        webView.isVisible = false
        webViewController.loadUrl(url)
    }

    private fun showSetupUi() {
        setPlaybackActive(false)
        val savedUrl = serverSettings.getServerUrl()
        setupContainer.isVisible = true
        cancelSetupButton.isVisible = savedUrl != null
        errorContainer.isVisible = false
        webView.isVisible = false
        serverUrlEditText.setText(savedUrl ?: "")
        serverUrlEditText.requestFocus()
    }

    private fun hideSetupUi() {
        setupContainer.isVisible = false
    }

    private fun showErrorUi(title: String, detail: String) {
        setPlaybackActive(false)
        errorTitle.text = title
        errorDetail.text = detail
        errorContainer.isVisible = true
        setupContainer.isVisible = false
        webView.isVisible = false
        retryButton.requestFocus()
    }

    private fun hideErrorUi() {
        errorContainer.isVisible = false
    }

    private fun handleMediaKey(keyCode: Int): Boolean {
        if (setupContainer.isVisible || errorContainer.isVisible) {
            return false
        }

        val action = when (keyCode) {
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE,
            KeyEvent.KEYCODE_HEADSETHOOK -> "playPause"
            KeyEvent.KEYCODE_MEDIA_PLAY -> "play"
            KeyEvent.KEYCODE_MEDIA_PAUSE -> "pause"
            KeyEvent.KEYCODE_MEDIA_STOP -> "stop"
            KeyEvent.KEYCODE_MEDIA_REWIND,
            KeyEvent.KEYCODE_MEDIA_PREVIOUS -> "seekBack"
            KeyEvent.KEYCODE_MEDIA_FAST_FORWARD,
            KeyEvent.KEYCODE_MEDIA_NEXT -> "seekForward"
            else -> return false
        }

        webViewController.dispatchHostMediaKey(action)
        return true
    }

    private fun applyPlaybackKeepScreenOn(active: Boolean) {
        if (active) {
            window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        } else {
            window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        }
    }

    private fun setPlaybackActive(active: Boolean) {
        playbackActive = active
        applyPlaybackKeepScreenOn(active && !isFinishing)
    }

    private fun detectTvDevice(): Boolean {
        val modeType = resources.configuration.uiMode and Configuration.UI_MODE_TYPE_MASK
        return modeType == Configuration.UI_MODE_TYPE_TELEVISION ||
            packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
    }

    private fun buildHostCapabilitiesJson(): String {
        return JSONObject()
            .put("platform", if (isTvDevice) "android-tv" else "android")
            .put("isTv", isTvDevice)
            .put("supportsKeepScreenAwake", true)
            .put("supportsHostMediaKeys", true)
            .put("supportsInputFocus", true)
            .toString()
    }

    private fun openExternal(uri: Uri) {
        val intent = Intent(Intent.ACTION_VIEW, uri)
        try {
            startActivity(intent)
        } catch (_: ActivityNotFoundException) {
        }
    }

    companion object {
        private const val STATE_LAST_REQUESTED_URL = "state_last_requested_url"
    }
}
