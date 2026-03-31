package io.github.manugh.xg2g.android

import android.content.ComponentCallbacks2
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class WebViewRecoveryPolicyTest {
    private val policy = WebViewRecoveryPolicy()

    @Test
    fun `recovery url prefers last requested url over base url`() {
        val recoveryUrl = policy.recoveryUrl(
            lastRequestedUrl = "https://app.example.invalid/player",
            currentBaseUrl = "https://app.example.invalid"
        )

        assertEquals("https://app.example.invalid/player", recoveryUrl)
    }

    @Test
    fun `recovery url falls back to base url when last requested is blank`() {
        val recoveryUrl = policy.recoveryUrl(
            lastRequestedUrl = "   ",
            currentBaseUrl = "https://app.example.invalid"
        )

        assertEquals("https://app.example.invalid", recoveryUrl)
    }

    @Test
    fun `renderer recovery is deferred when activity is not safely foregrounded`() {
        assertFalse(
            policy.shouldDeferRendererRecovery(
                isFinishing = false,
                isDestroyed = false,
                isStarted = true,
                hasWindowFocus = true
            )
        )

        assertTrue(
            policy.shouldDeferRendererRecovery(
                isFinishing = false,
                isDestroyed = false,
                isStarted = false,
                hasWindowFocus = true
            )
        )

        assertTrue(
            policy.shouldDeferRendererRecovery(
                isFinishing = false,
                isDestroyed = false,
                isStarted = true,
                hasWindowFocus = false
            )
        )
    }

    @Suppress("DEPRECATION")
    @Test
    fun `trim memory release only happens for recoverable background sessions`() {
        assertTrue(
            policy.shouldReleaseWebViewUnderTrimMemory(
                trimMemoryLevel = ComponentCallbacks2.TRIM_MEMORY_BACKGROUND,
                isSetupVisible = false,
                isErrorVisible = false,
                pendingRendererRecoveryUrl = null,
                shouldDeferRendererRecovery = true,
                recoveryUrl = "https://app.example.invalid/player"
            )
        )

        assertFalse(
            policy.shouldReleaseWebViewUnderTrimMemory(
                trimMemoryLevel = ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW,
                isSetupVisible = false,
                isErrorVisible = false,
                pendingRendererRecoveryUrl = null,
                shouldDeferRendererRecovery = true,
                recoveryUrl = "https://app.example.invalid/player"
            )
        )

        assertFalse(
            policy.shouldReleaseWebViewUnderTrimMemory(
                trimMemoryLevel = ComponentCallbacks2.TRIM_MEMORY_BACKGROUND,
                isSetupVisible = true,
                isErrorVisible = false,
                pendingRendererRecoveryUrl = null,
                shouldDeferRendererRecovery = true,
                recoveryUrl = "https://app.example.invalid/player"
            )
        )

        assertFalse(
            policy.shouldReleaseWebViewUnderTrimMemory(
                trimMemoryLevel = ComponentCallbacks2.TRIM_MEMORY_BACKGROUND,
                isSetupVisible = false,
                isErrorVisible = false,
                pendingRendererRecoveryUrl = "https://app.example.invalid/player",
                shouldDeferRendererRecovery = true,
                recoveryUrl = "https://app.example.invalid/player"
            )
        )
    }

    @Suppress("DEPRECATION")
    @Test
    fun `log helpers map trim levels and normalize urls`() {
        assertEquals(
            "RUNNING_LOW",
            policy.describeTrimMemoryLevel(ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW)
        )
        assertEquals("none", policy.describeUrlForLog(null))
        assertEquals("app.example.invalid", policy.describeUrlForLog("https://app.example.invalid/player"))
    }
}
