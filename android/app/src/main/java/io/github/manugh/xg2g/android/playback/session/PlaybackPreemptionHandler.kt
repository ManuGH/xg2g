package io.github.manugh.xg2g.android.playback.session

internal sealed interface PreemptionOutcome {
    object Active : PreemptionOutcome
    data class Preempted(val reason: String) : PreemptionOutcome
    data class Revoked(val reason: String) : PreemptionOutcome
    data class Expired(val reason: String) : PreemptionOutcome
}

internal class PlaybackPreemptionHandler {
    fun evaluateHttpStatus(statusCode: Int, message: String = ""): PreemptionOutcome {
        return when (statusCode) {
            409 -> PreemptionOutcome.Preempted(reason = message.ifBlank { "Stream wurde von höherer Priorität übernommen" })
            401, 403 -> PreemptionOutcome.Revoked(reason = message.ifBlank { "Geräte-Zugriff vom Admin widerrufen" })
            410 -> PreemptionOutcome.Expired(reason = message.ifBlank { "Wiedergabe-Sitzung ist abgelaufen" })
            else -> PreemptionOutcome.Active
        }
    }

    fun evaluatePushEvent(type: String, message: String = ""): PreemptionOutcome {
        return when (type.lowercase()) {
            "stream_preempted" -> PreemptionOutcome.Preempted(reason = message.ifBlank { "Stream wurde von höherer Priorität übernommen" })
            "device_revoked" -> PreemptionOutcome.Revoked(reason = message.ifBlank { "Geräte-Zugriff vom Admin widerrufen" })
            "session_expired" -> PreemptionOutcome.Expired(reason = message.ifBlank { "Wiedergabe-Sitzung ist abgelaufen" })
            else -> PreemptionOutcome.Active
        }
    }
}
