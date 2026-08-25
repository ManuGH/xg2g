package io.github.manugh.xg2g.android.transport.playback

import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import okhttp3.Response
import org.json.JSONObject

internal class PlaybackErrorMapper {
    fun toHttpException(response: Response, body: String?): PlaybackHttpException {
        val detail = extractProblemDetail(body)?.let { " · $it" }.orEmpty()
        return PlaybackHttpException(
            statusCode = response.code,
            message = "Playback API ${response.code}: ${response.message}$detail"
        )
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

internal class PlaybackHttpException(
    val statusCode: Int,
    message: String
) : IllegalStateException(message)
