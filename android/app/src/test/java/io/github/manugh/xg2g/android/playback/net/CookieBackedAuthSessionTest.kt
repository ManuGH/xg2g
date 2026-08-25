package io.github.manugh.xg2g.android.playback.net

import io.github.manugh.xg2g.android.transport.playback.sessionCookieDeletionPaths
import org.junit.Assert.assertEquals
import org.junit.Test

class CookieBackedAuthSessionTest {
    @Test
    fun `session cleanup deletes both api and cloned root cookies`() {
        assertEquals(
            listOf("/api/v3/", "/"),
            sessionCookieDeletionPaths("/api/v3/")
        )
    }

    @Test
    fun `root cookie cleanup is not duplicated`() {
        assertEquals(listOf("/"), sessionCookieDeletionPaths("/"))
    }
}
