package io.github.manugh.xg2g.android

import io.github.manugh.xg2g.android.transport.apiV3Url
import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.assertEquals
import org.junit.Test

class ApiV3UrlTest {

    @Test
    fun `thumbnail url is root anchored when base url carries the ui path`() {
        val url = apiV3Url(
            "https://host/ui/".toHttpUrl(),
            "recordings",
            "rec-1",
            "thumbnail.jpg"
        )

        assertEquals("https://host/api/v3/recordings/rec-1/thumbnail.jpg", url.toString())
    }

    @Test
    fun `ui base path is replaced rather than extended`() {
        val url = apiV3Url("https://host/ui/".toHttpUrl(), "recordings")

        assertEquals(listOf("api", "v3", "recordings"), url.pathSegments)
    }

    @Test
    fun `port and scheme of the configured base url are preserved`() {
        val url = apiV3Url(
            "http://host.local:8080/ui/".toHttpUrl(),
            "recordings",
            "rec-1",
            "thumbnail.jpg"
        )

        assertEquals(
            "http://host.local:8080/api/v3/recordings/rec-1/thumbnail.jpg",
            url.toString()
        )
    }

    @Test
    fun `query and fragment of the configured base url are dropped`() {
        val url = apiV3Url(
            "https://host/ui/?theme=dark#live".toHttpUrl(),
            "recordings",
            "rec-1",
            "thumbnail.jpg"
        )

        assertEquals("https://host/api/v3/recordings/rec-1/thumbnail.jpg", url.toString())
    }

    /**
     * Regression guard for deployment sub-paths.
     *
     * `ServerTargetResolver.extractUiBasePath` preserves a sub-path in the UI base URL
     * (`https://host/xg2g/ui/` stays intact), so a sub-path base URL can reach this helper. The
     * backend, however, mounts the v3 router at the origin root via the compile-time constant
     * `V3BaseURL = "/api/v3"` and serves the SPA through a hardcoded `StripPrefix("/ui")`; there is
     * no configurable deployment prefix. The API is therefore origin-rooted and the `/xg2g` prefix
     * is deliberately dropped.
     *
     * This test exists to make that decision explicit rather than incidental: if the backend ever
     * gains a configurable prefix, this test is what should fail and force the discussion.
     */
    @Test
    fun `deployment sub-path in the base url is dropped because the api is origin-rooted`() {
        val url = apiV3Url(
            "https://host/xg2g/ui/".toHttpUrl(),
            "recordings",
            "rec-1",
            "thumbnail.jpg"
        )

        assertEquals("https://host/api/v3/recordings/rec-1/thumbnail.jpg", url.toString())
    }

    @Test
    fun `path segments are encoded instead of splitting the path`() {
        val url = apiV3Url("https://host/ui/".toHttpUrl(), "recordings", "a b/c", "thumbnail.jpg")

        assertEquals(listOf("api", "v3", "recordings", "a b/c", "thumbnail.jpg"), url.pathSegments)
        assertEquals(
            "https://host/api/v3/recordings/a%20b%2Fc/thumbnail.jpg",
            url.toString()
        )
    }
}
