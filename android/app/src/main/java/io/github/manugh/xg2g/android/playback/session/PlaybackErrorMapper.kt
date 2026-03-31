package io.github.manugh.xg2g.android.playback.session

import io.github.manugh.xg2g.android.playback.model.SessionSnapshot
import okhttp3.Response

internal class PlaybackErrorMapper {
    fun toHttpException(response: Response, body: String?): IllegalStateException {
        val detail = body?.takeIf { it.isNotBlank() }?.let { " · $it" }.orEmpty()
        return IllegalStateException("Playback API ${response.code}: ${response.message}$detail")
    }

    fun toSessionStateException(snapshot: SessionSnapshot): IllegalStateException {
        return IllegalStateException(
            "Session ${snapshot.sessionId} entered terminal state ${snapshot.state.wireValue}"
        )
    }
}
