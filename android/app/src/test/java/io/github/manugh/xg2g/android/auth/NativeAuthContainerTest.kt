package io.github.manugh.xg2g.android.auth

import android.content.Context
import android.content.ContextWrapper
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Test

class NativeAuthContainerTest {

    private class FakeContext : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this
        override fun getSystemService(name: String): Any? = null
    }

    @Test
    fun `getInstance returns singleton instance`() {
        val context = FakeContext()
        val container1 = NativeAuthContainer.getInstance(context)
        val container2 = NativeAuthContainer.getInstance(context)

        assertSame(container1, container2)
        assertNotNull(container1)
    }
}
