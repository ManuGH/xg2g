package io.github.manugh.xg2g.android

import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ResolveInfo
import android.content.res.Configuration
import android.net.Uri
import android.os.Bundle
import android.util.Log
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
import io.github.manugh.xg2g.android.guide.GuideActivity
import io.github.manugh.xg2g.android.playback.PlaybackSessionRegistry
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.PlaybackJsonCodec
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.playback.net.NativePlaybackCapabilities
import io.github.manugh.xg2g.android.playback.net.PlaybackApiJsonCodec
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import org.json.JSONObject

class MainActivity : AppCompatActivity() {
    private lateinit var screenUi: MainScreenUi
    private lateinit var webViewController: WebViewHostController

    private var lastRequestedUrl: String = ""
    private var playbackActive = false
    private var sessionAuthToken: String? = null
    private var uiState: MainUiState = MainUiState.Loading()
    private var loadAppUrlJob: Job? = null

    private val serverSettingsStore by lazy { ServerSettingsStore(this) }
    private val deviceAuthRepository by lazy(LazyThreadSafetyMode.NONE) {
        DeviceAuthRepository(applicationContext)
    }
    private val nativePlaybackBridge by lazy(LazyThreadSafetyMode.NONE) { NativePlaybackBridge(this) }
    private val isTvDevice by lazy(LazyThreadSafetyMode.NONE) { detectTvDevice() }
    private val serializedHostCapabilities by lazy(LazyThreadSafetyMode.NONE) { buildHostCapabilitiesJson() }
    private val serializedPlaybackCapabilities by lazy(LazyThreadSafetyMode.NONE) {
        PlaybackApiJsonCodec.playbackCapabilitiesJson(
            NativePlaybackCapabilities.create(applicationContext)
        )
    }
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
            serializedPlaybackCapabilities = serializedPlaybackCapabilities,
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

                override fun isWebUiVisible(): Boolean {
                    return uiState == MainUiState.Content || uiState is MainUiState.Loading
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

        val existingBaseUrl = serverSettingsStore.getServerUrl()
        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = existingBaseUrl,
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )
        applyResolvedDeviceAuth(
            existingBaseUrl = existingBaseUrl,
            configuredBaseUrl = configuredBaseUrl,
            intent = intent
        )
        sessionAuthToken = resolveSessionAuthToken(
            existingBaseUrl = existingBaseUrl,
            configuredBaseUrl = configuredBaseUrl,
            intent = intent
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
                routeInitialDestination(
                    baseUrl = configuredBaseUrl,
                    startUrl = startUrl,
                    reason = "on_create"
                )
            } else {
                lastRequestedUrl = savedInstanceState.getString(STATE_LAST_REQUESTED_URL) ?: startUrl
                val restoredState = webViewController.restoreState(savedInstanceState)
                if (restoredState == null || webView.url.isNullOrBlank()) {
                    routeInitialDestination(
                        baseUrl = configuredBaseUrl,
                        startUrl = lastRequestedUrl,
                        reason = "restore_missing_webview_state"
                    )
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
        val existingBaseUrl = serverSettingsStore.getServerUrl()
        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = existingBaseUrl,
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )
        applyResolvedDeviceAuth(
            existingBaseUrl = existingBaseUrl,
            configuredBaseUrl = configuredBaseUrl,
            intent = intent
        )
        sessionAuthToken = resolveSessionAuthToken(
            existingBaseUrl = existingBaseUrl,
            configuredBaseUrl = configuredBaseUrl,
            intent = intent
        )
        if (configuredBaseUrl != null) {
            serverSettingsStore.saveServerUrl(configuredBaseUrl)
            val startUrl = ServerTargetResolver.resolveStartUrl(
                baseUrl = configuredBaseUrl,
                overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
                deepLinkUrl = intent.dataString
            )
            routeInitialDestination(
                baseUrl = configuredBaseUrl,
                startUrl = startUrl,
                reason = "on_new_intent"
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
        loadAppUrlJob?.cancel()
        webViewController.release(renderProcessGone = false)
        super.onDestroy()
    }

    private fun configureScreenUi() {
        screenUi.bindActions(
            onConnect = { input ->
                if (validateAndSaveUrl(input)) {
                    if (isTvDevice) {
                        showTvHomeUi(reason = "connect_server")
                    } else {
                        loadAppUrl(serverSettingsStore.getServerUrl()!!, reason = "connect_server")
                    }
                }
            },
            onCancelSetup = {
                val savedUrl = serverSettingsStore.getServerUrl()
                if (savedUrl != null) {
                    if (isTvDevice) {
                        showTvHomeUi(reason = "cancel_setup")
                    } else {
                        loadAppUrl(savedUrl, reason = "cancel_setup")
                    }
                }
            },
            onRetry = { loadAppUrl(lastRequestedUrl, reason = "error_retry") },
            onChangeServer = { showSetupUi() },
            onOpenWebTools = { openCurrentWebTools() },
            onOpenInBrowser = { openExternal(currentExternalUrl().toUri()) },
            onOpenTvMenu = { showTvQuickActions() },
            onOpenTvHome = { navigateToTvDestination(TvNavigationDestination.Home) },
            onOpenTvGuide = { openTvGuide() },
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
        deviceAuthRepository.clearPersistedState()
        sessionAuthToken = null
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
                    is MainUiState.TvHome -> {
                        backgroundTaskOrFinish()
                        return
                    }

                    is MainUiState.Setup -> {
                        val savedUrl = serverSettingsStore.getServerUrl()
                        if (savedUrl != null) {
                            if (isTvDevice) {
                                showTvHomeUi(reason = "back_from_setup")
                            } else {
                                loadAppUrl(savedUrl, reason = "back_from_setup")
                            }
                        } else {
                            backgroundTaskOrFinish()
                        }
                        return
                    }

                    is MainUiState.Error -> {
                        if (isTvDevice && serverSettingsStore.getServerUrl() != null) {
                            showTvHomeUi(reason = "back_from_error")
                        } else {
                            showSetupUi()
                        }
                        return
                    }

                    is MainUiState.Loading,
                    MainUiState.Content -> {
                        if (webViewController.canGoBack()) {
                            webViewController.goBack()
                            return
                        }

                        if (shouldReturnToTvHome()) {
                            showTvHomeUi(reason = "back_to_tv_home")
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

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.action == KeyEvent.ACTION_DOWN && screenUi.isTvQuickActionsVisible()) {
            screenUi.ensureTvQuickActionsFocus()
        }

        return super.dispatchKeyEvent(event)
    }

    private fun loadAppUrl(url: String, reason: String = "navigate") {
        setPlaybackActive(false)
        hideTvQuickActions(restoreFocus = false)
        lastRequestedUrl = url
        Log.i(
            TAG,
            "event=load_app_url_requested reason=$reason url=$url"
        )
        setUiState(MainUiState.Loading(destinationLabel = describeDestination(url)))
        loadAppUrlJob?.cancel()
        loadAppUrlJob = lifecycleScope.launch {
            Log.i(
                TAG,
                "event=prepare_web_ui_start reason=$reason url=$url"
            )
            val preparedUrl = runCatching {
                prepareWebUiUrl(url)
            }.getOrElse { error ->
                Log.w(
                    TAG,
                    "event=prepare_web_ui_failed reason=$reason url=$url message=${error.message}"
                )
                showErrorUi(
                    title = getString(R.string.webview_error_title),
                    detail = error.message ?: getString(R.string.webview_error_generic)
                )
                return@launch
            }
            if (lastRequestedUrl != url) {
                Log.i(
                    TAG,
                    "event=prepare_web_ui_discarded reason=$reason requestedUrl=$url latestUrl=$lastRequestedUrl"
                )
                return@launch
            }
            Log.i(
                TAG,
                "event=prepare_web_ui_complete reason=$reason requestedUrl=$url preparedUrl=$preparedUrl"
            )
            webViewController.loadUrl(preparedUrl)
        }
    }

    private fun showSetupUi() {
        setPlaybackActive(false)
        hideTvQuickActions(restoreFocus = false)
        setUiState(MainUiState.Setup(serverSettingsStore.getServerUrl()))
    }

    private fun showTvHomeUi(reason: String = "navigate") {
        val baseUrl = serverSettingsStore.getServerUrl()
        if (!isTvDevice || baseUrl.isNullOrBlank()) {
            showSetupUi()
            return
        }

        setPlaybackActive(false)
        hideTvQuickActions(restoreFocus = false)
        lastRequestedUrl = baseUrl
        Log.i(
            TAG,
            "event=show_tv_home reason=$reason baseUrl=$baseUrl"
        )
        setUiState(MainUiState.TvHome(serverLabel = describeServer(baseUrl)))
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
        if (uiState is MainUiState.TvHome || uiState is MainUiState.Setup || uiState is MainUiState.Error) {
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
            hasCustomView = webViewController.hasCustomView(),
            externalBrowserAvailable = canOpenExternalBrowser(currentExternalUrl())
        )

        if (newState == MainUiState.Content && !screenUi.isTvQuickActionsVisible()) {
            webViewController.requestInputFocus()
        }
    }

    private fun canOpenTvQuickActions(): Boolean {
        return isTvDevice && uiState == MainUiState.Content
    }

    private fun shouldReturnToTvHome(): Boolean {
        if (!isTvDevice || serverSettingsStore.getServerUrl() == null) {
            return false
        }
        if (uiState != MainUiState.Content && uiState !is MainUiState.Loading) {
            return false
        }
        return resolveActiveTvDestination(currentExternalUrl()) != null
    }

    private fun showTvQuickActions() {
        if (!canOpenTvQuickActions()) {
            return
        }
        screenUi.showTvQuickActions(
            context = describeDestination(currentExternalUrl()),
            activeDestination = resolveActiveTvDestination(currentExternalUrl())
        )
        screenUi.setExternalBrowserActionVisible(canOpenExternalBrowser(currentExternalUrl()))
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
        loadAppUrl(currentExternalUrl(), reason = "quick_reload")
    }

    private fun openCurrentWebTools() {
        val targetUrl = currentExternalUrl().takeIf { it.isNotBlank() }
            ?: serverSettingsStore.getServerUrl()

        if (targetUrl == null) {
            showSetupUi()
            return
        }

        Log.i(
            TAG,
            "event=open_web_tools targetUrl=$targetUrl"
        )
        loadAppUrl(targetUrl, reason = "open_web_tools")
    }

    private fun openTvGuide() {
        if (!isTvDevice) {
            navigateToTvDestination(TvNavigationDestination.Guide)
            return
        }

        hideTvQuickActions(restoreFocus = false)
        val baseUrl = serverSettingsStore.getServerUrl()
        if (baseUrl.isNullOrBlank()) {
            showSetupUi()
            return
        }

        startActivity(GuideActivity.createIntent(this, baseUrl, sessionAuthToken))
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
        loadAppUrl(targetUrl, reason = "tv_destination_${destination.name.lowercase()}")
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

    private fun describeServer(url: String): String {
        return describeDestination(url) ?: url
    }

    private fun shouldLaunchNativeTvHome(startUrl: String, baseUrl: String): Boolean {
        if (!isTvDevice) {
            return false
        }

        if (!ServerTargetResolver.isSameOrigin(startUrl, baseUrl)) {
            return false
        }

        val startPath = runCatching { startUrl.toUri().encodedPath.orEmpty().trimEnd('/') }.getOrDefault("")
        val basePath = runCatching { baseUrl.toUri().encodedPath.orEmpty().trimEnd('/') }.getOrDefault("")
        return startPath == basePath
    }

    private fun resolveSessionAuthToken(
        existingBaseUrl: String?,
        configuredBaseUrl: String?,
        intent: Intent
    ): String? {
        val explicitToken = ServerTargetResolver.resolveAuthToken(
            overrideToken = intent.getStringExtra(ServerTargetResolver.EXTRA_AUTH_TOKEN),
            deepLinkUrl = intent.dataString
        ) ?: ServerTargetResolver.resolveAccessToken(
            overrideToken = intent.getStringExtra(ServerTargetResolver.EXTRA_ACCESS_TOKEN),
            deepLinkUrl = intent.dataString
        )
        if (explicitToken != null) {
            return explicitToken
        }
        if (configuredBaseUrl != null && configuredBaseUrl != existingBaseUrl) {
            return null
        }
        return sessionAuthToken
    }

    private suspend fun prepareWebUiUrl(url: String): String {
        val baseUrl = serverSettingsStore.getServerUrl() ?: return url
        if (!ServerTargetResolver.isSameOrigin(url, baseUrl)) {
            return url
        }
        return deviceAuthRepository.prepareWebSession(
            baseUrl = baseUrl,
            targetUrl = url,
            legacyAuthToken = sessionAuthToken
        )
    }

    private fun applyResolvedDeviceAuth(
        existingBaseUrl: String?,
        configuredBaseUrl: String?,
        intent: Intent
    ) {
        if (configuredBaseUrl != null && configuredBaseUrl != existingBaseUrl) {
            Log.i(
                TAG,
                "event=device_auth_state_cleared reason=base_url_changed previousBaseUrl=$existingBaseUrl newBaseUrl=$configuredBaseUrl"
            )
            deviceAuthRepository.clearPersistedState()
        }
        if (configuredBaseUrl == null) {
            return
        }

        val launchCredentials = ServerTargetResolver.resolveDeviceAuthLaunchCredentials(
            overrideDeviceGrantId = intent.getStringExtra(ServerTargetResolver.EXTRA_DEVICE_GRANT_ID),
            overrideDeviceGrant = intent.getStringExtra(ServerTargetResolver.EXTRA_DEVICE_GRANT),
            overrideAccessToken = intent.getStringExtra(ServerTargetResolver.EXTRA_ACCESS_TOKEN),
            overrideAccessTokenExpiresAt = intent.getStringExtra(ServerTargetResolver.EXTRA_ACCESS_TOKEN_EXPIRES_AT),
            deepLinkUrl = intent.dataString
        )
        if (launchCredentials != null) {
            Log.i(
                TAG,
                "event=device_auth_launch_credentials_resolved hasGrant=${launchCredentials.hasPersistableGrant()} hasAccessToken=${!launchCredentials.accessToken.isNullOrBlank()} baseUrl=$configuredBaseUrl"
            )
        }
        deviceAuthRepository.applyLaunchCredentials(configuredBaseUrl, launchCredentials)
    }

    private fun routeInitialDestination(baseUrl: String, startUrl: String, reason: String) {
        val shouldLaunchTvHome = shouldLaunchNativeTvHome(startUrl, baseUrl)
        Log.i(
            TAG,
            "event=route_initial_destination reason=$reason isTv=$isTvDevice shouldLaunchTvHome=$shouldLaunchTvHome baseUrl=$baseUrl startUrl=$startUrl"
        )
        if (shouldLaunchTvHome) {
            showTvHomeUi(reason = reason)
        } else {
            loadAppUrl(startUrl, reason = reason)
        }
    }

    private fun openExternal(uri: Uri) {
        val intent = buildExternalIntent(uri)
        val handler = resolveExternalHandler(intent, requireBrowser = isNetworkBrowseUri(uri)) ?: return
        val defaultHandler = packageManager.resolveActivity(intent, PackageManager.MATCH_DEFAULT_ONLY)
        val launchIntent = if (matchesActivity(defaultHandler, handler)) {
            intent
        } else {
            Intent(intent).setClassName(handler.activityInfo.packageName, handler.activityInfo.name)
        }

        try {
            startActivity(launchIntent)
        } catch (_: ActivityNotFoundException) {
        }
    }

    private fun canOpenExternalBrowser(url: String): Boolean {
        if (!URLUtil.isNetworkUrl(url)) {
            return false
        }

        val intent = buildExternalIntent(url.toUri())
        return resolveExternalHandler(intent, requireBrowser = true) != null
    }

    @Suppress("DEPRECATION")
    private fun resolveExternalHandler(intent: Intent, requireBrowser: Boolean): ResolveInfo? {
        val defaultHandler = packageManager.resolveActivity(intent, PackageManager.MATCH_DEFAULT_ONLY)
        if (isUsableExternalHandler(defaultHandler, requireBrowser)) {
            return defaultHandler
        }

        return packageManager.queryIntentActivities(intent, PackageManager.MATCH_DEFAULT_ONLY)
            .firstOrNull { isUsableExternalHandler(it, requireBrowser) }
    }

    private fun isUsableExternalHandler(handler: ResolveInfo?, requireBrowser: Boolean): Boolean {
        val activityInfo = handler?.activityInfo ?: return false
        if (!requireBrowser) {
            return true
        }

        return ExternalBrowserPolicy.isUsableBrowserHandler(
            packageName = activityInfo.packageName,
            className = activityInfo.name
        )
    }

    private fun buildExternalIntent(uri: Uri): Intent {
        return Intent(Intent.ACTION_VIEW, uri).apply {
            addCategory(Intent.CATEGORY_BROWSABLE)
        }
    }

    private fun isNetworkBrowseUri(uri: Uri): Boolean {
        return uri.scheme in setOf("http", "https")
    }

    private fun matchesActivity(first: ResolveInfo?, second: ResolveInfo?): Boolean {
        val firstInfo = first?.activityInfo ?: return false
        val secondInfo = second?.activityInfo ?: return false
        return firstInfo.packageName == secondInfo.packageName && firstInfo.name == secondInfo.name
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
        private const val TAG = "Xg2gMainLaunch"
    }
}
