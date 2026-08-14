package io.github.manugh.xg2g.android

import okhttp3.HttpUrl

/**
 * Single source of truth for building `/api/v3` URLs from a configured server base URL.
 *
 * The configured base URL carries the SPA base path (`https://host/ui/`), but the REST API is
 * mounted at the origin root: the backend registers the v3 router under a wildcard below the
 * compile-time constant `V3BaseURL = "/api/v3"` (backend/internal/control/http/v3/baseurl.go), and
 * serves the SPA through a hardcoded `http.StripPrefix("/ui", ...)`. Neither is configurable, so
 * there is no deployment sub-path variant of the API.
 *
 * Consequently this is deliberately **origin-rooted**: [HttpUrl.Builder.encodedPath] with a leading
 * slash replaces the base path rather than appending to it, so any prefix the base URL carries
 * (`/ui/`, or a `/xg2g/ui/` sub-path) is dropped. Appending instead would produce
 * `https://host/ui//api/v3/...`, which the SPA handler answers with index.html or a 404.
 *
 * If the backend ever gains a configurable deployment prefix, this function is the one place that
 * has to learn about it — see `ApiV3UrlTest` for the tests that pin the current contract.
 */
internal fun apiV3Url(baseUrl: HttpUrl, vararg segments: String): HttpUrl =
    apiV3UrlBuilder(baseUrl, *segments).build()

/**
 * Builder variant of [apiV3Url] for call sites that still need to attach query parameters.
 */
internal fun apiV3UrlBuilder(baseUrl: HttpUrl, vararg segments: String): HttpUrl.Builder =
    baseUrl.newBuilder()
        .encodedPath("/api/v3/")
        .query(null)
        .fragment(null)
        .apply {
            segments.forEach(::addPathSegment)
        }
