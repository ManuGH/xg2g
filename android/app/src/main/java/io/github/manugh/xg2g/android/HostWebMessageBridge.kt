package io.github.manugh.xg2g.android

import android.annotation.SuppressLint
import android.net.Uri
import android.util.Log
import android.view.View
import android.webkit.WebView
import androidx.appcompat.app.AppCompatActivity
import androidx.webkit.JavaScriptReplyProxy
import androidx.webkit.WebMessageCompat
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import io.github.manugh.xg2g.android.bridge.HostBridgeContract
import io.github.manugh.xg2g.android.playback.model.NativePlaybackRequest
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

// Every WebMessage API entry is dominated by bindOrigin's feature check. Lint cannot follow the
// resulting reply-proxy invariant across callbacks, so keep the guard centralized and explicit.
@SuppressLint("RequiresFeature")
internal class HostWebMessageBridge(
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

    private var boundWebView: WebView? = null
    private var boundOrigin: String? = null
    private var replyProxy: JavaScriptReplyProxy? = null

    fun bindOrigin(target: WebView, url: String?) {
        val origin = webMessageOriginRule(url)
        if (boundWebView === target && boundOrigin == origin) {
            return
        }

        boundWebView?.let(::detach)
        if (origin == null) {
            Log.w(TAG, "Host bridge disabled: no valid HTTP(S) origin")
            return
        }
        if (!WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            Log.e(TAG, "Host bridge disabled: WebMessageListener is unavailable")
            return
        }

        WebViewCompat.addWebMessageListener(
            target,
            HostBridgeContract.BRIDGE_NAME,
            setOf(origin)
        ) { view, message, sourceOrigin, isMainFrame, responseProxy ->
            receiveMessage(
                target = view,
                message = message,
                sourceOrigin = sourceOrigin,
                isMainFrame = isMainFrame,
                responseProxy = responseProxy
            )
        }
        boundWebView = target
        boundOrigin = origin
        Log.i(TAG, "Origin-bound host bridge enabled for $origin")
    }

    fun detach(target: WebView) {
        if (boundWebView !== target) {
            return
        }
        if (WebViewFeature.isFeatureSupported(WebViewFeature.WEB_MESSAGE_LISTENER)) {
            runCatching { WebViewCompat.removeWebMessageListener(target, HostBridgeContract.BRIDGE_NAME) }
        }
        boundWebView = null
        boundOrigin = null
        replyProxy = null
    }

    fun onPageStarted(target: WebView) {
        if (boundWebView === target) {
            replyProxy = null
        }
    }

    fun publishSnapshot() {
        replyProxy?.postMessage(snapshotJson())
    }

    fun dispatchMediaKey(action: String, lastRequestedUrl: String) {
        if (lastRequestedUrl.isBlank()) {
            return
        }
        dispatch(HostBridgeContract.HostMediaKey(action))
    }

    fun requestInputFocus() {
        if (!callbacks.shouldRequestInputFocus()) {
            return
        }

        val activeWebView = activeWebView()
        activeWebView.post { activeWebView.requestFocus(View.FOCUS_DOWN) }
    }

    fun publishNativePlaybackState(stateJson: String) {
        dispatch(HostBridgeContract.NativePlaybackState(stateJson))
    }

    private fun receiveMessage(
        target: WebView,
        message: WebMessageCompat,
        sourceOrigin: Uri,
        isMainFrame: Boolean,
        responseProxy: JavaScriptReplyProxy
    ) {
        val expectedOrigin = boundOrigin
        if (target !== boundWebView || !isTrustedWebMessage(expectedOrigin, sourceOrigin.toString(), isMainFrame)) {
            Log.w(TAG, "Rejected host bridge message outside the bound main-frame origin")
            return
        }

        val rawMessage = message.data ?: return
        activity.runOnUiThread {
            runCatching { HostBridgeContract.parseCommand(rawMessage) }
                .onSuccess { command ->
                    if (command is HostBridgeContract.Command.Hello) {
                        replyProxy = responseProxy
                        Log.i(TAG, "Origin-bound host bridge handshake completed for $expectedOrigin")
                        responseProxy.postMessage(snapshotJson())
                        return@onSuccess
                    }
                    if (replyProxy == null) {
                        Log.w(TAG, "Rejected host bridge command before handshake")
                        return@onSuccess
                    }
                    replyProxy = responseProxy
                    handleCommand(command)
                }
                .onFailure { error ->
                    Log.w(TAG, "Rejected malformed host bridge message: ${error.message}")
                }
        }
    }

    private fun handleCommand(command: HostBridgeContract.Command) {
        when (command) {
            HostBridgeContract.Command.Hello -> Unit
            is HostBridgeContract.Command.SetPlaybackActive -> {
                callbacks.onPlaybackActiveChanged(command.active)
            }
            HostBridgeContract.Command.RequestInputFocus -> requestInputFocus()
            is HostBridgeContract.Command.StartNativePlayback -> {
                Log.d(
                    TAG,
                    "startNativePlayback kind=${command.request.javaClass.simpleName} hasAuthToken=${!command.request.authToken.isNullOrBlank()}"
                )
                callbacks.startNativePlayback(command.request)
                publishSnapshot()
            }
            HostBridgeContract.Command.StopNativePlayback -> {
                callbacks.stopNativePlayback()
                publishSnapshot()
            }
        }
    }

    private fun dispatch(event: HostBridgeContract.Event) {
        replyProxy?.postMessage(event.toMessageJson())
    }

    private fun snapshotJson(): String = HostBridgeContract.snapshotJson(
        serializedHostCapabilities = serializedHostCapabilities,
        serializedPlaybackCapabilities = serializedPlaybackCapabilities,
        nativePlaybackStateJson = callbacks.currentNativePlaybackStateJson()
    )

    private companion object {
        const val TAG = "Xg2gHostBridge"
    }
}

internal fun webMessageOriginRule(url: String?): String? {
    val parsed = url?.toHttpUrlOrNull() ?: return null
    if (parsed.scheme !in setOf("http", "https")) {
        return null
    }
    val host = if (':' in parsed.host) "[${parsed.host}]" else parsed.host
    val defaultPort = if (parsed.scheme == "https") 443 else 80
    val port = parsed.port.takeIf { it != defaultPort }?.let { ":$it" }.orEmpty()
    return "${parsed.scheme}://$host$port"
}

internal fun isTrustedWebMessage(expectedOrigin: String?, sourceOrigin: String?, isMainFrame: Boolean): Boolean =
    isMainFrame && expectedOrigin != null && webMessageOriginRule(sourceOrigin) == expectedOrigin
