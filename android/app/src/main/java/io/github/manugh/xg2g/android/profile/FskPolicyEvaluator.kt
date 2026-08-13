package io.github.manugh.xg2g.android.profile

import io.github.manugh.xg2g.android.dashboard.NativeHouseholdProfile

internal sealed interface FskDecision {
    object Permitted : FskDecision
    data class Locked(val reason: String, val contentFsk: Int, val maxAllowedFsk: Int) : FskDecision
    data class ApprovalEligible(val reason: String, val contentFsk: Int, val maxAllowedFsk: Int) : FskDecision
}

internal class FskPolicyEvaluator {
    fun evaluate(contentFsk: Int, profile: NativeHouseholdProfile?): FskDecision {
        if (profile == null) {
            return FskDecision.Permitted
        }
        if (profile.kind.lowercase() in setOf("admin", "adult", "owner")) {
            return FskDecision.Permitted
        }

        val maxAllowed = profile.maxFsk ?: 12
        if (contentFsk <= maxAllowed) {
            return FskDecision.Permitted
        }

        // FSK exceeds profile max
        val reason = "FSK $contentFsk - Für dieses Profil (max. FSK $maxAllowed) gesperrt"
        val canRequestApproval = profile.kind.lowercase() in setOf("child", "restricted", "teen", "kids")
        return if (canRequestApproval) {
            FskDecision.ApprovalEligible(reason = reason, contentFsk = contentFsk, maxAllowedFsk = maxAllowed)
        } else {
            FskDecision.Locked(reason = reason, contentFsk = contentFsk, maxAllowedFsk = maxAllowed)
        }
    }
}
