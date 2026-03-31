package io.github.manugh.xg2g.android.playback.model

data class SessionSnapshot(
    val sessionId: String,
    val state: SessionState,
    val playbackUrl: String?,
    val mode: SessionMode?,
    val requestId: String?,
    val profileReason: String?,
    val traceJson: String?,
    val heartbeatIntervalSec: Int?,
    val leaseExpiresAt: String?,
    val durationSeconds: Double?,
    val seekableStartSeconds: Double?,
    val seekableEndSeconds: Double?,
    val liveEdgeSeconds: Double?
)
