package io.github.manugh.xg2g.android.bridge

import android.webkit.WebView

internal class HostBridgeEventDispatcher(
    private val activeWebView: () -> WebView
) {
    fun dispatch(event: HostBridgeContract.Event) {
        runCatching { activeWebView().evaluateJavascript(event.toJavascript(), null) }
    }
}
