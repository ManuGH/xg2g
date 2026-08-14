package io.github.manugh.xg2g.android

import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ResolveInfo
import android.content.res.Configuration
import android.net.Uri
import android.os.Bundle
import androidx.activity.compose.setContent
import kotlinx.coroutines.flow.MutableStateFlow
import androidx.activity.viewModels
import io.github.manugh.xg2g.android.guide.GuideViewModel
import io.github.manugh.xg2g.android.dashboard.DashboardViewModel
import android.util.Log
import android.view.KeyEvent
import android.view.WindowManager
import android.webkit.URLUtil
import androidx.activity.OnBackPressedCallback
import androidx.appcompat.app.AlertDialog
import androidx.appcompat.app.AppCompatActivity
import androidx.core.net.toUri
import androidx.core.splashscreen.SplashScreen.Companion.installSplashScreen
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.lifecycleScope
import io.github.manugh.xg2g.android.auth.AndroidKeystoreDPoPProvider
import io.github.manugh.xg2g.android.auth.AuthStateMachine
import io.github.manugh.xg2g.android.auth.NativeDeviceAuthRepository
import io.github.manugh.xg2g.android.auth.NativeDeviceAuthTransport
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

    private var lastRequestedUrl: String = ""
    private var playbackActive = false
    private var sessionAuthToken: String? = null
    private var uiState: MainUiState = MainUiState.Loading()
    private var loadAppUrlJob: Job? = null
    private val destinationFlow = MutableStateFlow(TvNavigationDestination.Home)
    private val authContainer by lazy(LazyThreadSafetyMode.NONE) {
        io.github.manugh.xg2g.android.auth.NativeAuthContainer.getInstance(applicationContext)
    }
    private val nativeDeviceAuthRepository get() = authContainer.repository

    private val dashboardViewModel: DashboardViewModel by viewModels {
        DashboardViewModel.Factory(
            context = applicationContext,
            serverLabelProvider = { serverSettingsStore.getServerUrl()?.let { describeServer(it) } ?: "" },
            baseUrlProvider = { serverSettingsStore.getServerUrl() ?: "" },
            authTokenProvider = { sessionAuthToken ?: serverSettingsStore.getAuthToken() },
            stateStore = authContainer.stateStore,
            dpopProvider = authContainer.dpopProvider,
            stateMachine = authContainer.stateMachine
        )
    }

    private val guideViewModel: GuideViewModel by viewModels {
        GuideViewModel.Factory(
            context = applicationContext,
            serverLabelProvider = { serverSettingsStore.getServerUrl()?.let { describeServer(it) } ?: "" },
            baseUrlProvider = { serverSettingsStore.getServerUrl() ?: "" },
            authTokenProvider = { sessionAuthToken ?: serverSettingsStore.getAuthToken() },
            stateStore = authContainer.stateStore,
            dpopProvider = authContainer.dpopProvider,
            stateMachine = authContainer.stateMachine
        )
    }

    private val recordingsViewModel: io.github.manugh.xg2g.android.recordings.RecordingsViewModel by viewModels {
        io.github.manugh.xg2g.android.recordings.RecordingsViewModel.Factory(
            context = applicationContext,
            serverLabelProvider = { serverSettingsStore.getServerUrl()?.let { describeServer(it) } ?: "" },
            baseUrlProvider = { serverSettingsStore.getServerUrl() ?: "" },
            authTokenProvider = { sessionAuthToken ?: serverSettingsStore.getAuthToken() },
            stateStore = authContainer.stateStore,
            dpopProvider = authContainer.dpopProvider,
            stateMachine = authContainer.stateMachine
        )
    }

    private val timersViewModel: io.github.manugh.xg2g.android.timers.TimersViewModel by viewModels {
        io.github.manugh.xg2g.android.timers.TimersViewModel.Factory(
            context = applicationContext,
            serverLabelProvider = { serverSettingsStore.getServerUrl()?.let { describeServer(it) } ?: "" },
            baseUrlProvider = { serverSettingsStore.getServerUrl() ?: "" },
            authTokenProvider = { sessionAuthToken ?: serverSettingsStore.getAuthToken() },
            stateStore = authContainer.stateStore,
            dpopProvider = authContainer.dpopProvider,
            stateMachine = authContainer.stateMachine
        )
    }

    private val settingsViewModel: io.github.manugh.xg2g.android.settings.SettingsViewModel by viewModels {
        io.github.manugh.xg2g.android.settings.SettingsViewModel.Factory(
            context = applicationContext,
            serverUrl = serverSettingsStore.getServerUrl() ?: ""
        )
    }

    private val serverSettingsStore by lazy { ServerSettingsStore(this) }
    private val nativePlaybackBridge by lazy(LazyThreadSafetyMode.NONE) { NativePlaybackBridge(this) }
    private val isTvDevice by lazy(LazyThreadSafetyMode.NONE) { detectTvDevice() }
    private val serializedHostCapabilities by lazy(LazyThreadSafetyMode.NONE) { buildHostCapabilitiesJson() }
    private val serializedPlaybackCapabilities by lazy(LazyThreadSafetyMode.NONE) {
        PlaybackApiJsonCodec.playbackCapabilitiesJson(
            NativePlaybackCapabilities.create(applicationContext)
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        installSplashScreen()
        super.onCreate(savedInstanceState)
        
        val mainView = layoutInflater.inflate(R.layout.activity_main, null, false)
        screenUi = MainScreenUi(
            activity = this,
            view = mainView,
            isTvDevice = isTvDevice
        )

        setContent {
            MainActivityContent(
                destinationFlow = destinationFlow,
                onNavigate = { navigateToTvDestination(it) },
                dashboardViewModel = dashboardViewModel,
                guideViewModel = guideViewModel,
                recordingsViewModel = recordingsViewModel,
                timersViewModel = timersViewModel,
                settingsViewModel = settingsViewModel,
                assetBaseUrl = serverSettingsStore.getServerUrl() ?: "",
                onPlayChannel = { channel -> 
                    nativePlaybackBridge.start(
                        NativePlaybackRequest.Live(
                            serviceRef = channel.serviceRef,
                            title = channel.displayName,
                            logoUrl = channel.logoUrl,
                            authToken = sessionAuthToken ?: serverSettingsStore.getAuthToken(),
                            profile = "direct"
                        )
                    )
                },
                onPlayRecording = { item ->
                    val startPosMs = if (item.resume != null && item.resume.finished != true && item.resume.posSeconds > 0) {
                        item.resume.posSeconds * 1000L
                    } else 0L

                    val thumbnailUrl = serverSettingsStore.getServerUrl()?.let { base ->
                        "$base/api/v3/recordings/${item.recordingId}/thumbnail.jpg"
                    }

                    nativePlaybackBridge.start(
                        NativePlaybackRequest.Recording(
                            recordingId = item.recordingId,
                            startPositionMs = startPosMs,
                            title = item.title,
                            logoUrl = thumbnailUrl,
                            authToken = sessionAuthToken ?: serverSettingsStore.getAuthToken(),
                            profile = "direct"
                        )
                    )
                },
                onOpenSetup = { showSetupUi() },
                onExitGuide = { navigateToTvDestination(TvNavigationDestination.Home) }
            )
        }


        configureScreenUi()
        installBackHandler()

        applyIntentConfiguration(
            intent = intent,
            savedInstanceState = savedInstanceState,
            routeReason = "on_create"
        )
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        applyIntentConfiguration(
            intent = intent,
            savedInstanceState = null,
            routeReason = "on_new_intent"
        )
    }

    override fun onResume() {
        super.onResume()
        dashboardViewModel.refresh()
        recordingsViewModel.refresh(isInitial = false)
        applyPlaybackKeepScreenOn(playbackActive)
    }

    override fun onPause() {
        applyPlaybackKeepScreenOn(false)
        super.onPause()
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putString(STATE_LAST_REQUESTED_URL, lastRequestedUrl)
        super.onSaveInstanceState(outState)
    }

    override fun onDestroy() {
        loadAppUrlJob?.cancel()
        super.onDestroy()
    }

    private fun configureScreenUi() {
        screenUi.bindActions(
            onConnect = { input ->
                if (validateAndSaveUrl(input)) {
                    dashboardViewModel.refresh()
                    guideViewModel.refresh()
                    recordingsViewModel.refresh(isInitial = false)
                    timersViewModel.refresh(isInitial = false)
                    settingsViewModel.refreshHealth()
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
        nativeDeviceAuthRepository.clearPersistedState()
        sessionAuthToken = null
        return true
    }

    private fun installBackHandler() {
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                if (screenUi.isTvQuickActionsVisible()) {
                    hideTvQuickActions(restoreFocus = true)
                    return
                }
                
                // 1. Native Compose Screens (Guide, Recordings, Timers, Settings) -> Home Priority
                if (destinationFlow.value != TvNavigationDestination.Home) {
                    navigateToTvDestination(TvNavigationDestination.Home)
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

                    is MainUiState.Revoked,
                    is MainUiState.ReauthRequired,
                    is MainUiState.RefreshingBanner,
                    is MainUiState.Loading,
                    MainUiState.Content -> {
                        // 3. From other Root destination -> Home
                        if (destinationFlow.value != TvNavigationDestination.Home) {
                            navigateToTvDestination(TvNavigationDestination.Home)
                            return
                        }
                        
                        // 4. From Home -> Exit
                        if (shouldReturnToTvHome()) {
                            showTvHomeUi(reason = "back_to_tv_home")
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
            if (isTvDevice) {
                showTvHomeUi(reason = "prepare_web_ui_complete")
            } else {
                showSetupUi()
            }
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
        if (!isTvDevice) {
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
        return false
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
            externalBrowserAvailable = canOpenExternalBrowser(currentExternalUrl())
        )
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
        // TvQuickActions are now disabled in favor of the Compose BroadcastRail
        // If the user presses MENU, we could optionally open the BroadcastRail here,
        // but it is automatically focusable now.
    }

    private fun hideTvQuickActions(restoreFocus: Boolean) {
        if (!screenUi.isTvQuickActionsVisible()) {
            return
        }

        screenUi.hideTvQuickActions()
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
        navigateToTvDestination(TvNavigationDestination.Guide)
    }

    private fun navigateToTvDestination(destination: TvNavigationDestination) {
        destinationFlow.value = destination
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
        if (destination != TvNavigationDestination.Guide) {
            loadAppUrl(targetUrl, reason = "tv_destination_${destination.name.lowercase()}")
        }
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
            serverSettingsStore.saveAuthToken(explicitToken)
            return explicitToken
        }
        // Normalize before comparing so default-port differences do not falsely
        // suppress the legacy auth token on upgrade (see also applyResolvedDeviceAuth).
        val normalizedExisting = existingBaseUrl?.let(ServerTargetResolver::normalizeServerUrl)
        val normalizedConfigured = configuredBaseUrl?.let(ServerTargetResolver::normalizeServerUrl)
        if (normalizedConfigured != null && normalizedConfigured != normalizedExisting) {
            return null
        }
        return sessionAuthToken ?: serverSettingsStore.getAuthToken()
    }

    private suspend fun prepareWebUiUrl(url: String): String {
        val baseUrl = serverSettingsStore.getServerUrl() ?: return url
        if (!ServerTargetResolver.isSameOrigin(url, baseUrl)) {
            return url
        }
        return nativeDeviceAuthRepository.prepareWebSession(
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
        // Normalize both sides so that default-port differences (e.g. :443 vs stripped)
        // do not falsely trigger auth-state clearing. This is defensive — callers
        // already normalize through ServerSettingsStore.getServerUrl() and
        // ServerTargetResolver.resolveConfiguredBaseUrl(), but this ensures
        // correctness regardless of caller conventions.
        val normalizedExisting = existingBaseUrl?.let(ServerTargetResolver::normalizeServerUrl)
        val normalizedConfigured = configuredBaseUrl?.let(ServerTargetResolver::normalizeServerUrl)
        if (normalizedConfigured != null && normalizedConfigured != normalizedExisting) {
            Log.i(
                TAG,
                "event=device_auth_state_cleared reason=base_url_changed previousBaseUrl=$existingBaseUrl newBaseUrl=$configuredBaseUrl"
            )
            nativeDeviceAuthRepository.clearPersistedState()
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
        nativeDeviceAuthRepository.applyLaunchCredentials(configuredBaseUrl, launchCredentials)
        lifecycleScope.launch {
            try {
                nativeDeviceAuthRepository.hydratePersistedStateOnLaunch(configuredBaseUrl)
            } catch (e: Exception) {
                Log.w(TAG, "Failed to hydrate native device auth state on launch", e)
            }
        }
    }

    private fun applyIntentConfiguration(
        intent: Intent,
        savedInstanceState: Bundle?,
        routeReason: String
    ) {
        val existingBaseUrl = serverSettingsStore.getServerUrl()
        val configuredBaseUrl = ServerTargetResolver.resolveConfiguredBaseUrl(
            existingBaseUrl = existingBaseUrl,
            overrideUrl = intent.getStringExtra(ServerTargetResolver.EXTRA_BASE_URL),
            deepLinkUrl = intent.dataString
        )

        if (configuredBaseUrl == null) {
            showSetupUi()
            return
        }

        if (existingBaseUrl != null &&
            ServerTargetResolver.isServerSwitch(existingBaseUrl, configuredBaseUrl)
        ) {
            Log.i(
                TAG,
                "event=server_switch_prompted reason=$routeReason previousBaseUrl=$existingBaseUrl newBaseUrl=$configuredBaseUrl"
            )
            promptServerSwitchConfirmation(
                currentBaseUrl = existingBaseUrl,
                newBaseUrl = configuredBaseUrl,
                onAccept = {
                    Log.i(
                        TAG,
                        "event=server_switch_accepted previousBaseUrl=$existingBaseUrl newBaseUrl=$configuredBaseUrl"
                    )
                    commitIntentConfiguration(
                        intent = intent,
                        savedInstanceState = null,
                        existingBaseUrl = existingBaseUrl,
                        configuredBaseUrl = configuredBaseUrl,
                        routeReason = routeReason
                    )
                },
                onDecline = {
                    Log.w(
                        TAG,
                        "event=server_switch_declined previousBaseUrl=$existingBaseUrl rejectedBaseUrl=$configuredBaseUrl"
                    )
                    // Keep existing server; drop all intent-derived auth and start URL.
                    commitIntentConfiguration(
                        intent = Intent(),
                        savedInstanceState = savedInstanceState,
                        existingBaseUrl = existingBaseUrl,
                        configuredBaseUrl = existingBaseUrl,
                        routeReason = "${routeReason}_switch_declined"
                    )
                }
            )
            return
        }

        commitIntentConfiguration(
            intent = intent,
            savedInstanceState = savedInstanceState,
            existingBaseUrl = existingBaseUrl,
            configuredBaseUrl = configuredBaseUrl,
            routeReason = routeReason
        )
    }

    private fun commitIntentConfiguration(
        intent: Intent,
        savedInstanceState: Bundle?,
        existingBaseUrl: String?,
        configuredBaseUrl: String,
        routeReason: String
    ) {
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
        serverSettingsStore.saveServerUrl(configuredBaseUrl)
        dashboardViewModel.refresh()
        recordingsViewModel.refresh(isInitial = false)


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
                reason = routeReason
            )
            return
        }

        lastRequestedUrl = savedInstanceState.getString(STATE_LAST_REQUESTED_URL) ?: startUrl
        routeInitialDestination(
            baseUrl = configuredBaseUrl,
            startUrl = lastRequestedUrl,
            reason = "restore_state"
        )
    }

    private fun promptServerSwitchConfirmation(
        currentBaseUrl: String,
        newBaseUrl: String,
        onAccept: () -> Unit,
        onDecline: () -> Unit
    ) {
        if (isFinishing || isDestroyed) {
            onDecline()
            return
        }
        AlertDialog.Builder(this)
            .setTitle(R.string.server_switch_confirm_title)
            .setMessage(getString(R.string.server_switch_confirm_message, currentBaseUrl, newBaseUrl))
            .setPositiveButton(R.string.server_switch_confirm_accept) { dialog, _ ->
                dialog.dismiss()
                onAccept()
            }
            .setNegativeButton(R.string.server_switch_confirm_decline) { dialog, _ ->
                dialog.dismiss()
                onDecline()
            }
            .setOnCancelListener { onDecline() }
            .setCancelable(true)
            .show()
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
