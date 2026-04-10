package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import okhttp3.Response
import org.json.JSONObject

internal class PlaybackErrorMapper {
    fun toHttpException(response: Response, body: String?): IllegalStateException {
        val detail = extractProblemDetail(body)?.let { " · $it" }.orEmpty()
        return IllegalStateException("Playback API ${response.code}: ${response.message}$detail")
    }

    fun toSessionStateException(snapshot: SessionSnapshot): IllegalStateException {
        return IllegalStateException(
            "Session ${snapshot.sessionId} entered terminal state ${snapshot.state.wireValue}"
        )
    }

    private fun extractProblemDetail(body: String?): String? {
        val raw = body?.trim()?.takeIf { it.isNotEmpty() } ?: return null
        return runCatching {
            JSONObject(raw).optString("detail").takeIf { it.isNotBlank() }
        }.getOrNull() ?: raw
    }
}
