package io.github.manugh.xg2g.android

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ExternalBrowserPolicyTest {

    @Test
    fun `tv framework browser stub is not treated as a usable browser`() {
        assertFalse(
            ExternalBrowserPolicy.isUsableBrowserHandler(
                packageName = "com.android.tv.frameworkpackagestubs",
                className = "com.android.tv.frameworkpackagestubs.Stubs\$BrowserStub"
            )
        )
    }

    @Test
    fun `missing package name is not treated as a usable browser`() {
        assertFalse(
            ExternalBrowserPolicy.isUsableBrowserHandler(
                packageName = null,
                className = "com.android.chrome.Main"
            )
        )
    }

    @Test
    fun `real browser package remains allowed`() {
        assertTrue(
            ExternalBrowserPolicy.isUsableBrowserHandler(
                packageName = "com.android.chrome",
                className = "com.google.android.apps.chrome.Main"
            )
        )
    }
}
