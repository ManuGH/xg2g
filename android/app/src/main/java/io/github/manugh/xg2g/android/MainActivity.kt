package io.github.manugh.xg2g.android

import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.Configuration
import android.net.Uri
import android.os.Bundle
import android.view.KeyEvent
import android.view.WindowManager
import android.webkit.URLUtil
import android.webkit.WebView
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AppCompatActivity
import androidx.core.net.toUri
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import androidx.lifecycle.repeatOnLifecycle
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import kotlinx.coroutines.launch
import org.json.JSONObject

class MainActivity : AppCompatActivity() {
    private lateinit var screenUi: MainScreenUi
    private lateinit var webViewController: WebViewHostController

    private var lastRequestedUrl: String = ""
    private var playbackActive = false
    private var uiState: MainUiState = MainUiState.Loading()

    private val serverSettingsStore by lazy { ServerSettingsStore(this) }
    private val nativePlaybackBridge by lazy(LazyThreadSafetyMode.NONE) { NativePlaybackBridge(this) }
    private val isTvDevice by lazy(LazyThreadSafetyMode.NONE) { detectTvDevice() }
    private val serializedHostCapabilities by lazy(LazyThreadSafetyMode.NONE) { buildHostCapabilitiesJson() }
    private val webView: WebView
        get() = webViewController.activeWebView

    override fun onCreate(savedInstanceState: Bundle?) {
        installSplashScreen()
        super.onCreate(savedInstanceState)

        WebView.setWebContentsDebuggingEnabled(BuildConfig.WEBVIEW_DEBUGGING)

        setContentView(R.layout.activity_main)

        screenUi = MainScreenUi(
            activity = this,
            isTvDevice = isTvDevice
        )

        webViewController = WebViewHostController(
            activity = this,
            initialWebView = screenUi.initialWebView,
            rootContainer = screenUi.rootContainer,
            fullscreenContainer = screenUi.fullscreenContainer,
            isTvDevice = isTvDevice,
            appVersionName = BuildConfig.VERSION_NAME,
            serializedHostCapabilities = serializedHostCapabilities,
            callbacks = object : WebViewHostController.Callbacks {
                override fun currentBaseUrl(): String? = serverSettingsStore.getServerUrl()

                override fun lastRequestedUrl(): String = lastRequestedUrl

                override fun updateLastRequestedUrl(url: String) {
                    lastRequestedUrl = url
                }

                override fun onMainFrameVisible() {
                    setUiState(MainUiState.Content)
                }

                override fun onMainFrameError(title: String, detail: String) {
                    showErrorUi(title, detail)
                }

                override fun onPlaybackActiveChanged(active: Boolean) {
                    setPlaybackActive(active)
                }

                override fun startNativePlayback(request: NativePlaybackRequest) {
                    nativePlaybackBridge.start(request)
                }

                override fun stopNativePlayback() {
                    nativePlaybackBridge.stop()
                }

                override fun currentNativePlaybackStateJson(): String {
                    return PlaybackSessionRegistry.currentStateJson()
                }

                override fun isSetupVisible(): Boolean = uiState is MainUiState.Setup

                override fun isErrorVisible(): Boolean = uiState is MainUiState.Error

                override fun openExternal(uri: Uri) {
                    this@MainActivity.openExternal(uri)
                }
            }
        )

        configureScreenUi()
        installBackHandler()
        observeNativePlaybackState()

        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = serverSettingsStore.getServerUrl(),
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )
        if (configuredBaseUrl != null) {
            serverSettingsStore.saveServerUrl(configuredBaseUrl)
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
                    setUiState(MainUiState.Content)
                }
            }
        } else {
            showSetupUi()
        }
    }

    private fun observeNativePlaybackState() {
        lifecycleScope.launch {
            repeatOnLifecycle(Lifecycle.State.STARTED) {
                PlaybackSessionRegistry.state.collect { state ->
                    webViewController.publishNativePlaybackState(PlaybackJsonCodec.stateToJson(state))
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = serverSettingsStore.getServerUrl(),
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )
        if (configuredBaseUrl != null) {
            serverSettingsStore.saveServerUrl(configuredBaseUrl)
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

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        webViewController.onTrimMemory(level)
    }

    override fun onLowMemory() {
        super.onLowMemory()
        webViewController.onLowMemory()
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

    private fun configureScreenUi() {
        screenUi.bindActions(
            onConnect = { input ->
                if (validateAndSaveUrl(input)) {
                    loadAppUrl(serverSettingsStore.getServerUrl()!!)
                }
            },
            onCancelSetup = {
                val savedUrl = serverSettingsStore.getServerUrl()
                if (savedUrl != null) {
                    loadAppUrl(savedUrl)
                }
            },
            onRetry = { loadAppUrl(lastRequestedUrl) },
            onChangeServer = { showSetupUi() },
            onOpenInBrowser = { openExternal(currentExternalUrl().toUri()) },
            onOpenTvMenu = { showTvQuickActions() },
            onOpenTvHome = { navigateToTvDestination(TvNavigationDestination.Home) },
            onOpenTvGuide = { navigateToTvDestination(TvNavigationDestination.Guide) },
            onOpenTvRecordings = { navigateToTvDestination(TvNavigationDestination.Recordings) },
            onOpenTvTimers = { navigateToTvDestination(TvNavigationDestination.Timers) },
            onOpenTvSettings = { navigateToTvDestination(TvNavigationDestination.Settings) },
            onQuickReload = {
                hideTvQuickActions(restoreFocus = false)
                reloadCurrentPage()
            },
            onQuickChangeServer = {
                hideTvQuickActions(restoreFocus = false)
                showSetupUi()
            },
            onQuickOpenInBrowser = {
                hideTvQuickActions(restoreFocus = false)
                openExternal(currentExternalUrl().toUri())
            },
            onQuickExit = {
                hideTvQuickActions(restoreFocus = false)
                backgroundTaskOrFinish()
            }
        )
    }

    private fun validateAndSaveUrl(input: String): Boolean {
        val normalizedUrl = ServerTargetResolver.normalizeServerUrl(input)
        if (normalizedUrl == null) {
            screenUi.showServerUrlError(getString(R.string.server_setup_invalid_url))
            return false
        }
        screenUi.clearServerUrlError()
        serverSettingsStore.saveServerUrl(normalizedUrl)
        return true
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (webViewController.hasCustomView()) {
                    webViewController.hideCustomView()
                    return
                }

                if (screenUi.isTvQuickActionsVisible()) {
                    hideTvQuickActions(restoreFocus = true)
                    return
                }

                when (uiState) {
                    is MainUiState.Setup -> {
                        val savedUrl = serverSettingsStore.getServerUrl()
                        if (savedUrl != null) {
                            loadAppUrl(savedUrl)
                        } else {
                            backgroundTaskOrFinish()
                        }
                        return
                    }

                    is MainUiState.Error -> {
                        showSetupUi()
                        return
                    }

                    is MainUiState.Loading,
                    MainUiState.Content -> {
                        if (webViewController.canGoBack()) {
                            webViewController.goBack()
                            return
                        }

                        if (canOpenTvQuickActions()) {
                            showTvQuickActions()
                            return
                        }
                    }
                }

                backgroundTaskOrFinish()
            }
        })
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent?): Boolean {
        if (keyCode == KeyEvent.KEYCODE_BACK) {
            onBackPressedDispatcher.onBackPressed()
            return true
        }
        if (event?.action == KeyEvent.ACTION_DOWN && event.repeatCount == 0) {
            if (handleAppControlKey(keyCode)) {
                return true
            }
            if (handleMediaKey(keyCode)) {
                return true
            }
        }
        return super.onKeyDown(keyCode, event)
    }

    private fun loadAppUrl(url: String) {
        setPlaybackActive(false)
        hideTvQuickActions(restoreFocus = false)
        lastRequestedUrl = url
        setUiState(MainUiState.Loading(destinationLabel = describeDestination(url)))
        webViewController.loadUrl(url)
    }

    private fun showSetupUi() {
        setPlaybackActive(false)
        hideTvQuickActions(restoreFocus = false)
        setUiState(MainUiState.Setup(serverSettingsStore.getServerUrl()))
    }

    private fun showErrorUi(title: String, detail: String) {
        setPlaybackActive(false)
        hideTvQuickActions(restoreFocus = false)
        setUiState(MainUiState.Error(title = title, detail = detail))
    }

    private fun handleAppControlKey(keyCode: Int): Boolean {
        if (!isTvDevice || webViewController.hasCustomView()) {
            return false
        }

        return when (keyCode) {
            KeyEvent.KEYCODE_MENU,
            KeyEvent.KEYCODE_SETTINGS -> {
                if (screenUi.isTvQuickActionsVisible()) {
                    hideTvQuickActions(restoreFocus = true)
                } else if (canOpenTvQuickActions()) {
                    showTvQuickActions()
                } else {
                    return false
                }
                true
            }

            else -> false
        }
    }

    private fun handleMediaKey(keyCode: Int): Boolean {
        if (uiState is MainUiState.Setup || uiState is MainUiState.Error) {
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
            .put("supportsNativePlayback", true)
            .toString()
    }

    private fun setUiState(newState: MainUiState) {
        uiState = newState
        screenUi.render(
            state = newState,
            webView = webView,
            hasCustomView = webViewController.hasCustomView()
        )

        if (newState == MainUiState.Content && !screenUi.isTvQuickActionsVisible()) {
            webViewController.requestInputFocus()
        }
    }

    private fun canOpenTvQuickActions(): Boolean {
        return isTvDevice && uiState == MainUiState.Content
    }

    private fun showTvQuickActions() {
        if (!canOpenTvQuickActions()) {
            return
        }
        screenUi.showTvQuickActions(
            context = describeDestination(currentExternalUrl()),
            activeDestination = resolveActiveTvDestination(currentExternalUrl())
        )
    }

    private fun hideTvQuickActions(restoreFocus: Boolean) {
        if (!screenUi.isTvQuickActionsVisible()) {
            return
        }

        screenUi.hideTvQuickActions()
        if (restoreFocus && uiState == MainUiState.Content) {
            webViewController.requestInputFocus()
        }
    }

    private fun reloadCurrentPage() {
        loadAppUrl(currentExternalUrl())
    }

    private fun navigateToTvDestination(destination: TvNavigationDestination) {
        val targetUrl = buildTvDestinationUrl(destination)
        if (targetUrl == null) {
            showSetupUi()
            return
        }

        if (resolveActiveTvDestination(currentExternalUrl()) == destination) {
            hideTvQuickActions(restoreFocus = true)
            return
        }

        hideTvQuickActions(restoreFocus = false)
        loadAppUrl(targetUrl)
    }

    private fun currentExternalUrl(): String {
        return lastRequestedUrl.takeIf { URLUtil.isNetworkUrl(it) }
            ?: serverSettingsStore.getServerUrl()
            ?: ""
    }

    private fun buildTvDestinationUrl(destination: TvNavigationDestination): String? {
        val baseUrl = serverSettingsStore.getServerUrl() ?: return null
        val baseUri = baseUrl.toUri()
        val basePath = baseUri.encodedPath
            ?.takeIf { it.isNotBlank() }
            ?.let { if (it.endsWith("/")) it else "$it/" }
            ?: "/ui/"
        val routePath = destination.routePath.removePrefix("/")
        return baseUri.buildUpon()
            .encodedPath(basePath + routePath)
            .encodedQuery(null)
            .fragment(null)
            .build()
            .toString()
    }

    private fun resolveActiveTvDestination(url: String): TvNavigationDestination? {
        if (url.isBlank()) {
            return null
        }

        val targetUri = runCatching { url.toUri() }.getOrNull() ?: return null
        val path = targetUri.encodedPath.orEmpty()

        return when {
            path.contains("/recordings") -> TvNavigationDestination.Recordings
            path.contains("/timers") -> TvNavigationDestination.Timers
            path.contains("/settings") -> TvNavigationDestination.Settings
            path.contains("/dashboard") -> TvNavigationDestination.Home
            path.contains("/epg") || path.endsWith("/ui") || path.endsWith("/ui/") -> TvNavigationDestination.Guide
            else -> null
        }
    }

    private fun describeDestination(url: String): String? {
        if (url.isBlank()) {
            return null
        }

        val targetUri = runCatching { url.toUri() }.getOrNull() ?: return url
        val host = targetUri.host ?: return url
        val path = targetUri.encodedPath?.takeIf { it.isNotBlank() && it != "/" }
        return if (path != null) "$host$path" else host
    }

    private fun openExternal(uri: Uri) {
        val intent = Intent(Intent.ACTION_VIEW, uri)
        try {
            startActivity(intent)
        } catch (_: ActivityNotFoundException) {
        }
    }

    private fun backgroundTaskOrFinish() {
        if (isTvDevice) {
            val homeIntent = Intent(Intent.ACTION_MAIN).apply {
                addCategory(Intent.CATEGORY_HOME)
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            try {
                startActivity(homeIntent)
                return
            } catch (_: ActivityNotFoundException) {
            }

            if (moveTaskToBack(true)) {
                return
            }
        }

        finish()
    }

    companion object {
        private const val STATE_LAST_REQUESTED_URL = "state_last_requested_url"
    }
}
