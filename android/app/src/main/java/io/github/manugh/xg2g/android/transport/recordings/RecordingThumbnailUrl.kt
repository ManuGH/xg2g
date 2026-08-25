package io.github.manugh.xg2g.android.transport.recordings

import io.github.manugh.xg2g.android.transport.apiV3Url
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/**
 * The single recording-specific source for thumbnail URLs.
 *
 * Kept as a top-level function rather than a [RecordingsApiClient] method so the UI can build the
 * URL without constructing an authenticated HTTP client just to format a path.
 */
internal fun recordingThumbnailUrl(baseUrl: HttpUrl, recordingId: String): HttpUrl =
    apiV3Url(baseUrl, "recordings", recordingId, "thumbnail.jpg")

/**
 * Overload for callers that hold the deployment address as text.
 *
 * Parsing it is transport's job: a screen that wants a thumbnail should not
 * have to import an HTTP URL type to ask for one.
 */
internal fun recordingThumbnailUrl(baseUrl: String, recordingId: String): String? =
    baseUrl.toHttpUrlOrNull()?.let { recordingThumbnailUrl(it, recordingId).toString() }
