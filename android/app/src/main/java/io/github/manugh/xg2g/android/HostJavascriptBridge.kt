package io.github.manugh.xg2g.android

import android.view.View
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.appcompat.app.AppCompatActivity
import org.json.JSONObject

internal class HostJavascriptBridge(
    private val activity: AppCompatActivity,
    private val hostCapabilitiesJson: String,
    private val currentWebView: () -> WebView,
    private val callbacks: Callbacks
) {
    interface Callbacks {
        fun onPlaybackActiveChanged(active: Boolean)
        fun shouldRequestInputFocus(): Boolean
    }

    fun attach(target: WebView) {
        target.addJavascriptInterface(JavascriptApi(), JS_BRIDGE_NAME)
    }

    fun detach(target: WebView) {
        runCatching { target.removeJavascriptInterface(JS_BRIDGE_NAME) }
    }

    fun publishEnvironment(target: WebView) {
        val bridgeJson = JSONObject.quote(hostCapabilitiesJson)
        val script = """
            (() => {
              try {
                const host = JSON.parse($bridgeJson);
                window.__XG2G_HOST__ = host;
                window.dispatchEvent(new CustomEvent('xg2g:host-ready', { detail: host }));
              } catch (_) {
              }
            })();
        """.trimIndent()
        runCatching { target.evaluateJavascript(script, null) }
    }

    fun dispatchMediaKey(action: String, lastRequestedUrl: String) {
        if (lastRequestedUrl.isBlank()) {
            return
        }

        val escapedAction = JSONObject.quote(action)
        val script = """
            (() => {
              window.dispatchEvent(new CustomEvent('xg2g:host-media-key', {
                detail: { action: $escapedAction, ts: Date.now() }
              }));
            })();
        """.trimIndent()
        runCatching { currentWebView().evaluateJavascript(script, null) }
    }

    fun requestInputFocus() {
        if (!callbacks.shouldRequestInputFocus()) {
            return
        }

        val webView = currentWebView()
        webView.post { webView.requestFocus(View.FOCUS_DOWN) }
    }

    private inner class JavascriptApi {
        @JavascriptInterface
        fun getCapabilitiesJson(): String = hostCapabilitiesJson

        @JavascriptInterface
        fun setPlaybackActive(active: Boolean) {
            activity.runOnUiThread { callbacks.onPlaybackActiveChanged(active) }
        }

        @JavascriptInterface
        fun requestInputFocus() {
            activity.runOnUiThread { this@HostJavascriptBridge.requestInputFocus() }
        }
    }

    private companion object {
        private const val JS_BRIDGE_NAME = "Xg2gHost"
    }
}
