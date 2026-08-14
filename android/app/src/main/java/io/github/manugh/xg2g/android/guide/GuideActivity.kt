package io.github.manugh.xg2g.android.guide

import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.appcompat.app.AppCompatActivity
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import io.github.manugh.xg2g.android.playback.bridge.NativePlaybackBridge
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import io.github.manugh.xg2g.android.ui.theme.GuideTheme

class GuideActivity : AppCompatActivity() {
    private lateinit var baseUrl: String
    private var authToken: String? = null
    internal val playbackBridge by lazy(LazyThreadSafetyMode.NONE) { NativePlaybackBridge(this) }
    internal val viewModel: GuideViewModel by viewModels {
        val authContainer = io.github.manugh.xg2g.android.auth.NativeAuthContainer.getInstance(applicationContext)
        GuideViewModel.Factory(
            context = applicationContext,
            serverLabelProvider = { describeServer(baseUrl) },
            baseUrlProvider = { baseUrl },
            authTokenProvider = { authToken },
            stateStore = authContainer.stateStore,
            dpopProvider = authContainer.dpopProvider,
            stateMachine = authContainer.stateMachine
        )
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val store = io.github.manugh.xg2g.android.ServerSettingsStore(this)
        baseUrl = intent.getStringExtra(EXTRA_BASE_URL).orEmpty().ifBlank { store.getServerUrl().orEmpty() }
        authToken = intent.getStringExtra(EXTRA_AUTH_TOKEN)?.trim()?.takeIf { it.isNotEmpty() } ?: store.getAuthToken()
        if (baseUrl.isBlank()) {
            finish()
            return
        }

        setContent {
            val state by viewModel.state.collectAsStateWithLifecycle()
            GuideTheme {
                GuideScreen(
                    state = state,
                    assetBaseUrl = baseUrl,
                    onSelectBouquet = viewModel::selectBouquet,
                    onSelectChannel = viewModel::selectChannel,
                    onRefresh = viewModel::refresh,
                    onPlayChannel = ::playChannel,
                    onExit = ::finish
                )
            }
        }
    }

    internal fun playChannel(channel: GuideChannel) {
        playbackBridge.start(
            NativePlaybackRequest.Live(
                serviceRef = channel.serviceRef,
                title = channel.displayName,
                logoUrl = channel.logoUrl,
                authToken = authToken,
                profile = "direct"
            )
        )
    }

    internal fun describeServer(url: String): String {
        val uri = runCatching { Uri.parse(url) }.getOrNull()
        val host = uri?.host ?: return url
        val path = uri.path?.trim('/').orEmpty()
        return if (path.isNotBlank()) "$host/$path" else host
    }

    companion object {
        private const val EXTRA_BASE_URL = "guide_base_url"
        private const val EXTRA_AUTH_TOKEN = "guide_auth_token"

        fun createIntent(
            context: Context,
            baseUrl: String,
            authToken: String?
        ): Intent = Intent(context, GuideActivity::class.java).apply {
            putExtra(EXTRA_BASE_URL, baseUrl)
            authToken?.takeIf { it.isNotBlank() }?.let { putExtra(EXTRA_AUTH_TOKEN, it) }
        }
    }
}
