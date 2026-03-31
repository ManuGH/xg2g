package io.github.manugh.xg2g.android.playback.bridge

import android.webkit.WebView
import io.github.manugh.xg2g.android.bridge.HostBridgeContract
import io.github.manugh.xg2g.android.bridge.HostBridgeEventDispatcher

internal class NativePlaybackEventDispatcher(
    private val activeWebView: () -> WebView
) {
    private val bridgeEvents = HostBridgeEventDispatcher(activeWebView)

    fun dispatchState(stateJson: String) {
        bridgeEvents.dispatch(HostBridgeContract.NativePlaybackState(stateJson))
    }
}
