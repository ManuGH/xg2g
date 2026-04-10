package io.github.manugh.xg2g.android

import android.content.res.ColorStateList
import android.view.View
import android.webkit.WebView
import android.widget.FrameLayout
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import androidx.core.view.isVisible
import com.google.android.material.button.MaterialButton
import com.google.android.material.textfield.TextInputEditText
import com.google.android.material.textfield.TextInputLayout

internal class MainScreenUi(
    private val activity: AppCompatActivity,
    private val isTvDevice: Boolean
) {
    val rootContainer: FrameLayout = activity.findViewById(R.id.root_container)
    val fullscreenContainer: FrameLayout = activity.findViewById(R.id.fullscreen_container)
    val initialWebView: WebView = activity.findViewById(R.id.webview)

    private val loadingContainer: View = activity.findViewById(R.id.loading_container)
    private val loadingDetail: TextView = activity.findViewById(R.id.loading_detail)

    private val tvHomeContainer: View = activity.findViewById(R.id.tv_home_container)
    private val tvHomeServerValue: TextView = activity.findViewById(R.id.tv_home_server_value)
    private val tvHomeLiveButton: MaterialButton = activity.findViewById(R.id.tv_home_live_button)
    private val tvHomeDashboardButton: MaterialButton = activity.findViewById(R.id.tv_home_dashboard_button)
    private val tvHomeRecordingsButton: MaterialButton = activity.findViewById(R.id.tv_home_recordings_button)
    private val tvHomeTimersButton: MaterialButton = activity.findViewById(R.id.tv_home_timers_button)
    private val tvHomeSettingsButton: MaterialButton = activity.findViewById(R.id.tv_home_settings_button)
    private val tvHomeChangeServerButton: MaterialButton = activity.findViewById(R.id.tv_home_change_server_button)
    private val tvHomeBrowserButton: MaterialButton = activity.findViewById(R.id.tv_home_browser_button)

    private val setupContainer: View = activity.findViewById(R.id.setup_container)
    private val serverUrlLayout: TextInputLayout = activity.findViewById(R.id.server_url_layout)
    private val serverUrlEditText: TextInputEditText = activity.findViewById(R.id.server_url_edit_text)
    private val connectButton: MaterialButton = activity.findViewById(R.id.connect_button)
    private val cancelSetupButton: MaterialButton = activity.findViewById(R.id.cancel_setup_button)

    private val errorContainer: View = activity.findViewById(R.id.error_container)
    private val errorTitle: TextView = activity.findViewById(R.id.error_title)
    private val errorDetail: TextView = activity.findViewById(R.id.error_detail)
    private val retryButton: MaterialButton = activity.findViewById(R.id.retry_button)
    private val changeServerButton: MaterialButton = activity.findViewById(R.id.change_server_button)
    private val openInBrowserButton: MaterialButton = activity.findViewById(R.id.open_in_browser_button)

    private val tvQuickActionsContainer: View = activity.findViewById(R.id.tv_quick_actions_container)
    private val tvMenuButton: MaterialButton = activity.findViewById(R.id.tv_menu_button)
    private val tvQuickActionsContext: TextView = activity.findViewById(R.id.tv_quick_actions_context)
    private val tvHomeDestinationButton: MaterialButton = activity.findViewById(R.id.tv_home_destination_button)
    private val tvGuideDestinationButton: MaterialButton = activity.findViewById(R.id.tv_guide_destination_button)
    private val tvRecordingsDestinationButton: MaterialButton = activity.findViewById(R.id.tv_recordings_destination_button)
    private val tvTimersDestinationButton: MaterialButton = activity.findViewById(R.id.tv_timers_destination_button)
    private val tvSettingsDestinationButton: MaterialButton = activity.findViewById(R.id.tv_settings_destination_button)
    private val tvReloadButton: MaterialButton = activity.findViewById(R.id.tv_reload_button)
    private val tvChangeServerButton: MaterialButton = activity.findViewById(R.id.tv_change_server_button)
    private val tvOpenInBrowserButton: MaterialButton = activity.findViewById(R.id.tv_open_in_browser_button)
    private val tvExitButton: MaterialButton = activity.findViewById(R.id.tv_exit_button)

    private val activeRailBackground = color(R.color.ide_blue)
    private val activeRailStroke = color(R.color.ide_blue)
    private val activeRailText = color(R.color.ide_text_primary)
    private val idleRailBackground = color(R.color.ide_surface_panel_soft)
    private val idleRailStroke = color(R.color.ide_outline_soft)
    private val idleRailText = color(R.color.ide_text_primary)
    private val actionRailBackground = color(R.color.ide_surface_panel)
    private val actionRailStroke = color(R.color.ide_outline_soft)
    private val actionRailText = color(R.color.ide_text_secondary)
    private var preferredQuickActionsFocusTarget: MaterialButton? = null

    fun bindActions(
        onConnect: (String) -> Unit,
        onCancelSetup: () -> Unit,
        onRetry: () -> Unit,
        onChangeServer: () -> Unit,
        onOpenWebTools: () -> Unit,
        onOpenInBrowser: () -> Unit,
        onOpenTvMenu: () -> Unit,
        onOpenTvHome: () -> Unit,
        onOpenTvGuide: () -> Unit,
        onOpenTvRecordings: () -> Unit,
        onOpenTvTimers: () -> Unit,
        onOpenTvSettings: () -> Unit,
        onQuickReload: () -> Unit,
        onQuickChangeServer: () -> Unit,
        onQuickOpenInBrowser: () -> Unit,
        onQuickExit: () -> Unit
    ) {
        connectButton.setOnClickListener {
            onConnect(serverUrlEditText.text?.toString()?.trim().orEmpty())
        }
        cancelSetupButton.setOnClickListener { onCancelSetup() }
        tvHomeLiveButton.setOnClickListener { onOpenTvGuide() }
        tvHomeDashboardButton.setOnClickListener { onOpenTvHome() }
        tvHomeRecordingsButton.setOnClickListener { onOpenTvRecordings() }
        tvHomeTimersButton.setOnClickListener { onOpenTvTimers() }
        tvHomeSettingsButton.setOnClickListener { onOpenTvSettings() }
        tvHomeChangeServerButton.setOnClickListener { onChangeServer() }
        tvHomeBrowserButton.setOnClickListener { onOpenWebTools() }
        retryButton.setOnClickListener { onRetry() }
        changeServerButton.setOnClickListener { onChangeServer() }
        openInBrowserButton.setOnClickListener { onOpenInBrowser() }
        tvMenuButton.setOnClickListener { onOpenTvMenu() }
        tvHomeDestinationButton.setOnClickListener { onOpenTvHome() }
        tvGuideDestinationButton.setOnClickListener { onOpenTvGuide() }
        tvRecordingsDestinationButton.setOnClickListener { onOpenTvRecordings() }
        tvTimersDestinationButton.setOnClickListener { onOpenTvTimers() }
        tvSettingsDestinationButton.setOnClickListener { onOpenTvSettings() }
        tvReloadButton.setOnClickListener { onQuickReload() }
        tvChangeServerButton.setOnClickListener { onQuickChangeServer() }
        tvOpenInBrowserButton.setOnClickListener { onQuickOpenInBrowser() }
        tvExitButton.setOnClickListener { onQuickExit() }
    }

    fun showServerUrlError(message: String) {
        serverUrlLayout.error = message
    }

    fun clearServerUrlError() {
        serverUrlLayout.error = null
    }

    fun isTvQuickActionsVisible(): Boolean = tvQuickActionsContainer.isVisible

    fun showTvQuickActions(context: String?, activeDestination: TvNavigationDestination?) {
        if (!isTvDevice) {
            return
        }

        tvQuickActionsContext.text = context?.takeIf { it.isNotBlank() }
            ?.let { activity.getString(R.string.tv_quick_actions_context, it) }
            ?: activity.getString(R.string.tv_quick_actions_context_fallback)
        updateDestinationStyles(activeDestination)
        updateActionStyles()
        tvMenuButton.isVisible = false
        tvQuickActionsContainer.isVisible = true
        preferredQuickActionsFocusTarget = buttonForDestination(activeDestination)
        preferredQuickActionsFocusTarget?.requestFocus()
    }

    fun setExternalBrowserActionVisible(visible: Boolean) {
        openInBrowserButton.isVisible = visible
        tvOpenInBrowserButton.isVisible = visible
    }

    fun hideTvQuickActions() {
        tvQuickActionsContainer.isVisible = false
        tvMenuButton.isVisible = isTvDevice
    }

    fun ensureTvQuickActionsFocus() {
        if (!tvQuickActionsContainer.isVisible) {
            return
        }

        val currentFocus = activity.currentFocus
        if (currentFocus != null && isDescendantOf(tvQuickActionsContainer, currentFocus)) {
            return
        }

        val fallbackTarget = listOfNotNull(
            preferredQuickActionsFocusTarget,
            tvHomeDestinationButton,
            tvGuideDestinationButton,
            tvRecordingsDestinationButton,
            tvTimersDestinationButton,
            tvSettingsDestinationButton,
            tvReloadButton,
            tvExitButton,
        ).firstOrNull { it.isShown && it.isEnabled }

        fallbackTarget?.requestFocus()
    }

    private fun buttonForDestination(destination: TvNavigationDestination?): MaterialButton {
        return when (destination ?: TvNavigationDestination.Guide) {
            TvNavigationDestination.Home -> tvHomeDestinationButton
            TvNavigationDestination.Guide -> tvGuideDestinationButton
            TvNavigationDestination.Recordings -> tvRecordingsDestinationButton
            TvNavigationDestination.Timers -> tvTimersDestinationButton
            TvNavigationDestination.Settings -> tvSettingsDestinationButton
        }
    }

    private fun updateDestinationStyles(activeDestination: TvNavigationDestination?) {
        applyRailStyle(tvHomeDestinationButton, activeDestination == TvNavigationDestination.Home)
        applyRailStyle(tvGuideDestinationButton, activeDestination == TvNavigationDestination.Guide)
        applyRailStyle(tvRecordingsDestinationButton, activeDestination == TvNavigationDestination.Recordings)
        applyRailStyle(tvTimersDestinationButton, activeDestination == TvNavigationDestination.Timers)
        applyRailStyle(tvSettingsDestinationButton, activeDestination == TvNavigationDestination.Settings)
    }

    private fun updateActionStyles() {
        applyActionStyle(tvReloadButton)
        applyActionStyle(tvChangeServerButton)
        applyActionStyle(tvOpenInBrowserButton)
        applyActionStyle(tvExitButton)
    }

    private fun applyRailStyle(button: MaterialButton, active: Boolean) {
        button.backgroundTintList = ColorStateList.valueOf(if (active) activeRailBackground else idleRailBackground)
        button.strokeColor = ColorStateList.valueOf(if (active) activeRailStroke else idleRailStroke)
        button.strokeWidth = if (active) 0 else 1
        button.setTextColor(if (active) activeRailText else idleRailText)
        button.alpha = if (active) 1f else 0.94f
    }

    private fun applyActionStyle(button: MaterialButton) {
        button.backgroundTintList = ColorStateList.valueOf(actionRailBackground)
        button.strokeColor = ColorStateList.valueOf(actionRailStroke)
        button.strokeWidth = 1
        button.setTextColor(actionRailText)
        button.alpha = 0.92f
    }

    fun render(
        state: MainUiState,
        webView: WebView,
        hasCustomView: Boolean,
        externalBrowserAvailable: Boolean
    ) {
        setExternalBrowserActionVisible(externalBrowserAvailable)
        when (state) {
            is MainUiState.TvHome -> renderTvHome(state, webView)
            is MainUiState.Setup -> renderSetup(state, webView)
            is MainUiState.Error -> renderError(state, webView)
            is MainUiState.Loading -> renderLoading(state, webView)
            MainUiState.Content -> renderContent(webView, hasCustomView)
        }
    }

    private fun renderTvHome(state: MainUiState.TvHome, webView: WebView) {
        clearServerUrlError()
        hideTvQuickActions()
        loadingContainer.isVisible = false
        setupContainer.isVisible = false
        errorContainer.isVisible = false
        tvMenuButton.isVisible = false
        tvHomeContainer.isVisible = true
        webView.isVisible = false
        tvHomeServerValue.text = state.serverLabel
        tvHomeLiveButton.requestFocus()
    }

    private fun renderSetup(state: MainUiState.Setup, webView: WebView) {
        clearServerUrlError()
        hideTvQuickActions()
        loadingContainer.isVisible = false
        tvHomeContainer.isVisible = false
        tvMenuButton.isVisible = false
        setupContainer.isVisible = true
        cancelSetupButton.isVisible = state.savedUrl != null
        errorContainer.isVisible = false
        webView.isVisible = false
        serverUrlEditText.setText(state.savedUrl ?: "")

        if (isTvDevice && state.savedUrl != null) {
            connectButton.requestFocus()
            return
        }

        serverUrlEditText.post {
            serverUrlEditText.requestFocus()
            serverUrlEditText.text?.length?.let(serverUrlEditText::setSelection)
        }
    }

    private fun renderError(state: MainUiState.Error, webView: WebView) {
        hideTvQuickActions()
        loadingContainer.isVisible = false
        tvHomeContainer.isVisible = false
        tvMenuButton.isVisible = false
        errorTitle.text = state.title
        errorDetail.text = state.detail
        errorContainer.isVisible = true
        setupContainer.isVisible = false
        webView.isVisible = false
        retryButton.requestFocus()
    }

    private fun renderLoading(state: MainUiState.Loading, webView: WebView) {
        hideTvQuickActions()
        tvMenuButton.isVisible = false
        tvHomeContainer.isVisible = false
        setupContainer.isVisible = false
        errorContainer.isVisible = false
        loadingContainer.isVisible = true
        webView.isVisible = false
        loadingDetail.text = state.destinationLabel?.takeIf { it.isNotBlank() }
            ?.let { activity.getString(R.string.shell_loading_detail_host, it) }
            ?: activity.getString(R.string.shell_loading_detail_generic)
    }

    private fun renderContent(webView: WebView, hasCustomView: Boolean) {
        loadingContainer.isVisible = false
        tvHomeContainer.isVisible = false
        setupContainer.isVisible = false
        errorContainer.isVisible = false
        tvMenuButton.isVisible = isTvDevice && !tvQuickActionsContainer.isVisible
        webView.isVisible = !hasCustomView
    }

    private fun color(resId: Int): Int = ContextCompat.getColor(activity, resId)

    private fun isDescendantOf(container: View, candidate: View): Boolean {
        var current: View? = candidate
        while (current != null) {
            if (current === container) {
                return true
            }
            current = current.parent as? View
        }
        return false
    }
}
