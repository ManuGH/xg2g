package io.github.manugh.xg2g.android

import android.util.Log
import android.view.View
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.appcompat.app.AppCompatActivity
import io.github.manugh.xg2g.android.bridge.HostBridgeContract
import io.github.manugh.xg2g.android.bridge.HostBridgeEventDispatcher
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest

internal class HostJavascriptBridge(
    private val activity: AppCompatActivity,
    private val serializedHostCapabilities: String,
    private val serializedPlaybackCapabilities: String,
    private val activeWebView: () -> WebView,
    private val callbacks: Callbacks
) {
    interface Callbacks {
        fun onPlaybackActiveChanged(active: Boolean)
        fun shouldRequestInputFocus(): Boolean
        fun startNativePlayback(request: NativePlaybackRequest)
        fun stopNativePlayback()
        fun currentNativePlaybackStateJson(): String
    }

    private val bridgeEvents = HostBridgeEventDispatcher(activeWebView)

    fun attach(target: WebView) {
        target.addJavascriptInterface(JavascriptApi(), HostBridgeContract.BRIDGE_NAME)
    }

    fun detach(target: WebView) {
        runCatching { target.removeJavascriptInterface(HostBridgeContract.BRIDGE_NAME) }
    }

    fun publishEnvironment(target: WebView) {
        runCatching {
            target.evaluateJavascript(
                HostBridgeContract.HostReady(serializedHostCapabilities).toJavascript(),
                null
            )
        }
        publishCurrentNativePlaybackState()
    }

    fun dispatchMediaKey(action: String, lastRequestedUrl: String) {
        if (lastRequestedUrl.isBlank()) {
            return
        }

        bridgeEvents.dispatch(HostBridgeContract.HostMediaKey(action))
    }

    fun requestInputFocus() {
        if (!callbacks.shouldRequestInputFocus()) {
            return
        }

        val activeWebView = activeWebView()
        activeWebView.post { activeWebView.requestFocus(View.FOCUS_DOWN) }
    }

    fun publishCurrentNativePlaybackState() {
        publishNativePlaybackState(callbacks.currentNativePlaybackStateJson())
    }

    fun publishNativePlaybackState(stateJson: String) {
        bridgeEvents.dispatch(HostBridgeContract.NativePlaybackState(stateJson))
    }

    private fun handleCommand(command: HostBridgeContract.Command) {
        when (command) {
            is HostBridgeContract.Command.SetPlaybackActive -> {
                callbacks.onPlaybackActiveChanged(command.active)
            }

            HostBridgeContract.Command.RequestInputFocus -> {
                requestInputFocus()
            }

            is HostBridgeContract.Command.StartNativePlayback -> {
                Log.d(
                    TAG,
                    "startNativePlayback kind=${command.request.javaClass.simpleName} hasAuthToken=${!command.request.authToken.isNullOrBlank()}"
                )
                callbacks.startNativePlayback(command.request)
                publishCurrentNativePlaybackState()
            }

            HostBridgeContract.Command.StopNativePlayback -> {
                callbacks.stopNativePlayback()
                publishCurrentNativePlaybackState()
            }
        }
    }

    private inner class JavascriptApi {
        @JavascriptInterface
        fun getCapabilitiesJson(): String = serializedHostCapabilities

        @JavascriptInterface
        fun getPlaybackCapabilitiesJson(): String = serializedPlaybackCapabilities

        @JavascriptInterface
        fun setPlaybackActive(active: Boolean) {
            activity.runOnUiThread {
                handleCommand(HostBridgeContract.Command.SetPlaybackActive(active))
            }
        }

        @JavascriptInterface
        fun requestInputFocus() {
            activity.runOnUiThread {
                handleCommand(HostBridgeContract.Command.RequestInputFocus)
            }
        }

        @JavascriptInterface
        fun startNativePlayback(requestJson: String) {
            activity.runOnUiThread {
                handleCommand(HostBridgeContract.Command.StartNativePlayback.parse(requestJson))
            }
        }

        @JavascriptInterface
        fun stopNativePlayback() {
            activity.runOnUiThread {
                handleCommand(HostBridgeContract.Command.StopNativePlayback)
            }
        }

        @JavascriptInterface
        fun getNativePlaybackStateJson(): String = callbacks.currentNativePlaybackStateJson()
    }

    private companion object {
        const val TAG = "Xg2gHostBridge"
    }
}
