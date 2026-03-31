package io.github.manugh.xg2g.android

import android.view.View
import android.view.ViewGroup
import android.webkit.WebChromeClient
import android.webkit.WebView
import android.widget.FrameLayout
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat
import androidx.core.view.isVisible

internal class WebViewFullscreenController(
    private val activity: AppCompatActivity,
    private val rootContainer: FrameLayout,
    private val fullscreenContainer: FrameLayout,
    private val activeWebView: () -> WebView,
    private val shouldShowWebView: () -> Boolean
) {
    private var customView: View? = null
    private var customViewCallback: WebChromeClient.CustomViewCallback? = null

    val hasCustomView: Boolean
        get() = customView != null

    fun showCustomView(view: View?, callback: WebChromeClient.CustomViewCallback?) {
        if (view == null) {
            callback?.onCustomViewHidden()
            return
        }

        if (hasCustomView) {
            hideCustomView(notifyCallback = true)
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
        activeWebView().isVisible = false
        enterFullscreen()
    }

    fun hideCustomView(notifyCallback: Boolean) {
        val activeCustomView = customView ?: return
        fullscreenContainer.removeView(activeCustomView)
        fullscreenContainer.isVisible = false
        activeWebView().isVisible = shouldShowWebView()
        customView = null
        val callback = customViewCallback
        customViewCallback = null
        if (notifyCallback) {
            callback?.onCustomViewHidden()
        }
        exitFullscreen()
    }

    fun onPageCommitVisible() {
        if (!hasCustomView) {
            activeWebView().isVisible = shouldShowWebView()
        }
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
}
