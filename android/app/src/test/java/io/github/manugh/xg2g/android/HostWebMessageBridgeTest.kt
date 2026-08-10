package io.github.manugh.xg2g.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class HostWebMessageBridgeTest {
    @Test
    fun `origin rule strips paths credentials query and fragment`() {
        assertEquals(
            "https://xg2g.home.matrixcentral.de",
            webMessageOriginRule("https://user:secret@xg2g.home.matrixcentral.de/ui/?next=guide#now")
        )
    }

    @Test
    fun `origin rule preserves non-default ports`() {
        assertEquals("http://10.10.55.7:8080", webMessageOriginRule("http://10.10.55.7:8080/ui/"))
    }

    @Test
    fun `origin rule rejects non-network URLs`() {
        assertNull(webMessageOriginRule("file:///android_asset/index.html"))
        assertNull(webMessageOriginRule("javascript:alert(1)"))
        assertNull(webMessageOriginRule(null))
    }

    @Test
    fun `only the bound main-frame origin is trusted`() {
        val expected = "https://xg2g.home.matrixcentral.de"

        assertTrue(isTrustedWebMessage(expected, "$expected/ui/", isMainFrame = true))
        assertFalse(isTrustedWebMessage(expected, "$expected/ui/", isMainFrame = false))
        assertFalse(isTrustedWebMessage(expected, "https://attacker.example/ui/", isMainFrame = true))
        assertFalse(isTrustedWebMessage(null, "$expected/ui/", isMainFrame = true))
    }
}
