package io.github.manugh.xg2g.android

import android.content.ComponentCallbacks2
import java.net.URI

internal class WebViewRecoveryPolicy {
    fun recoveryUrl(lastRequestedUrl: String, currentBaseUrl: String?): String? {
        return lastRequestedUrl.takeIf { it.isNotBlank() }
            ?: currentBaseUrl?.takeIf { it.isNotBlank() }
    }

    fun shouldDeferRendererRecovery(
        isFinishing: Boolean,
        isDestroyed: Boolean,
        isStarted: Boolean,
        hasWindowFocus: Boolean
    ): Boolean {
        if (isFinishing || isDestroyed) {
            return true
        }

        return !isStarted || !hasWindowFocus
    }

    fun shouldReleaseWebViewUnderTrimMemory(
        trimMemoryLevel: Int,
        isSetupVisible: Boolean,
        isErrorVisible: Boolean,
        pendingRendererRecoveryUrl: String?,
        shouldDeferRendererRecovery: Boolean,
        recoveryUrl: String?
    ): Boolean {
        if (trimMemoryLevel < ComponentCallbacks2.TRIM_MEMORY_BACKGROUND) {
            return false
        }
        if (isSetupVisible || isErrorVisible) {
            return false
        }
        if (pendingRendererRecoveryUrl != null) {
            return false
        }
        if (!shouldDeferRendererRecovery) {
            return false
        }

        return recoveryUrl != null
    }

    @Suppress("DEPRECATION")
    fun describeTrimMemoryLevel(level: Int?): String {
        return when (level) {
            null -> "none"
            ComponentCallbacks2.TRIM_MEMORY_UI_HIDDEN -> "UI_HIDDEN"
            ComponentCallbacks2.TRIM_MEMORY_RUNNING_MODERATE -> "RUNNING_MODERATE"
            ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW -> "RUNNING_LOW"
            ComponentCallbacks2.TRIM_MEMORY_RUNNING_CRITICAL -> "RUNNING_CRITICAL"
            ComponentCallbacks2.TRIM_MEMORY_BACKGROUND -> "BACKGROUND"
            ComponentCallbacks2.TRIM_MEMORY_MODERATE -> "MODERATE"
            ComponentCallbacks2.TRIM_MEMORY_COMPLETE -> "COMPLETE"
            else -> level.toString()
        }
    }

    fun describeUrlForLog(url: String?): String {
        if (url.isNullOrBlank()) {
            return "none"
        }

        return runCatching { URI(url).host ?: url }.getOrDefault(url)
    }
}
