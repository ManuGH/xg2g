package io.github.manugh.xg2g.android.recordings

import io.github.manugh.xg2g.android.apiV3Url
import okhttp3.HttpUrl

/**
 * The single recording-specific source for thumbnail URLs.
 *
 * Kept as a top-level function rather than a [RecordingsApiClient] method so the UI can build the
 * URL without constructing an authenticated HTTP client just to format a path.
 */
internal fun recordingThumbnailUrl(baseUrl: HttpUrl, recordingId: String): HttpUrl =
    apiV3Url(baseUrl, "recordings", recordingId, "thumbnail.jpg")
