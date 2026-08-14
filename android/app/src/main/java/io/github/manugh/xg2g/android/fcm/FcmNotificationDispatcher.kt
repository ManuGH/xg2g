package io.github.manugh.xg2g.android.fcm

import io.github.manugh.xg2g.android.auth.AuthState
import io.github.manugh.xg2g.android.auth.AuthStateMachine

internal data class FcmPushPayload(
    val notificationId: String,
    val title: String,
    val body: String,
    val type: String, // "approval_request" | "device_revoked" | "session_expired" | "recording_event"
    val resourceId: String? = null,
    val actionRequired: String? = null
)

internal class FcmNotificationDispatcher(
    private val stateMachine: AuthStateMachine,
    private val onApprovalRequestReceived: ((FcmPushPayload) -> Unit)? = null,
    private val onRecordingEventReceived: ((FcmPushPayload) -> Unit)? = null
) {
    fun dispatchIncomingPush(payload: FcmPushPayload): AuthState {
        return when (payload.type.lowercase()) {
            "device_revoked" -> {
                stateMachine.revoke(reason = payload.body.ifBlank { "Admin revoked device access via FCM push" })
            }
            "session_expired" -> {
                stateMachine.handleRefreshError(
                    errorMsg = payload.body.ifBlank { "Session expired via FCM push" },
                    isNetworkError = false,
                    isRevoked = false
                )
            }
            "approval_request" -> {
                onApprovalRequestReceived?.invoke(payload)
                stateMachine.currentState
            }
            "recording_event" -> {
                onRecordingEventReceived?.invoke(payload)
                stateMachine.currentState
            }
            else -> {
                stateMachine.currentState
            }
        }
    }
}
