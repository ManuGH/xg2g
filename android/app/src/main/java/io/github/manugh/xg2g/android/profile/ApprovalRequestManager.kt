package io.github.manugh.xg2g.android.profile

import java.util.concurrent.ConcurrentHashMap

internal sealed interface ApprovalStatus {
    object None : ApprovalStatus
    data class Pending(val requestId: String, val resourceId: String, val requestedAtEpochMs: Long) : ApprovalStatus
    data class Approved(val requestId: String, val resourceId: String) : ApprovalStatus
    data class Rejected(val requestId: String, val resourceId: String, val reason: String) : ApprovalStatus
}

internal class ApprovalRequestManager(
    private val nowEpochMs: () -> Long = { System.currentTimeMillis() }
) {
    private val requests = ConcurrentHashMap<String, ApprovalStatus>()

    fun getStatus(resourceId: String): ApprovalStatus {
        return requests[resourceId] ?: ApprovalStatus.None
    }

    /**
     * Submit an approval request for content.
     * Prevents duplicate submissions if a request for this resource is already pending.
     */
    fun submitRequest(resourceId: String, requestIdGenerator: () -> String = { "req_" + System.currentTimeMillis() }): ApprovalStatus {
        val current = requests[resourceId]
        if (current is ApprovalStatus.Pending) {
            return current // Return existing pending request, DO NOT submit duplicate!
        }

        val reqId = requestIdGenerator()
        val newPending = ApprovalStatus.Pending(
            requestId = reqId,
            resourceId = resourceId,
            requestedAtEpochMs = nowEpochMs()
        )
        requests[resourceId] = newPending
        return newPending
    }

    /**
     * Called when Admin approves or rejects via FCM push / backend response.
     */
    fun resolveRequest(resourceId: String, requestId: String, approved: Boolean, reason: String = ""): ApprovalStatus {
        val nextStatus = if (approved) {
            ApprovalStatus.Approved(requestId = requestId, resourceId = resourceId)
        } else {
            ApprovalStatus.Rejected(requestId = requestId, resourceId = resourceId, reason = reason.ifBlank { "Freigabe abgelehnt" })
        }
        requests[resourceId] = nextStatus
        return nextStatus
    }

    fun clear(resourceId: String) {
        requests.remove(resourceId)
    }
}
