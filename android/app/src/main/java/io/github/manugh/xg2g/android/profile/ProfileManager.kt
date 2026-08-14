package io.github.manugh.xg2g.android.profile

import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.auth.DPoPProvider
import io.github.manugh.xg2g.android.dashboard.NativeHouseholdProfile
import java.util.concurrent.atomic.AtomicReference

internal interface ProfileSelectionStore {
    fun getSelectedProfileId(): String?
    fun saveSelectedProfileId(profileId: String?)
}

internal class ProfileManager(
    private val profileSelectionStore: ProfileSelectionStore,
    private val deviceAuthStore: PersistedDeviceAuthStateStore,
    private val dpopProvider: DPoPProvider
) {
    private val activeProfileRef = AtomicReference<NativeHouseholdProfile?>(null)

    val activeProfile: NativeHouseholdProfile?
        get() = activeProfileRef.get()

    val activeProfileId: String?
        get() = activeProfileRef.get()?.id ?: profileSelectionStore.getSelectedProfileId()

    /**
     * Switch current viewer profile.
     * STRICT SECURITY INVARIANT: Modifies ONLY viewer profile context.
     * MUST NEVER mutate, delete, or re-generate stored device grant, device key, or DPoP thumbprint!
     */
    fun selectProfile(profile: NativeHouseholdProfile): Boolean {
        // Capture device auth state BEFORE profile change
        val preDeviceState = deviceAuthStore.load()
        val preDPoPThumbprint = dpopProvider.getJWKThumbprint()

        // Apply profile change
        activeProfileRef.set(profile)
        profileSelectionStore.saveSelectedProfileId(profile.id)

        // Capture device auth state AFTER profile change
        val postDeviceState = deviceAuthStore.load()
        val postDPoPThumbprint = dpopProvider.getJWKThumbprint()

        // ENFORCE SECURITY INVARIANT
        check(preDPoPThumbprint == postDPoPThumbprint) {
            "SECURITY VIOLATION: Profile switch mutated DPoP key thumbprint!"
        }
        check(preDeviceState?.deviceGrantId == postDeviceState?.deviceGrantId) {
            "SECURITY VIOLATION: Profile switch mutated device grant ID!"
        }
        check(preDeviceState?.deviceGrant == postDeviceState?.deviceGrant) {
            "SECURITY VIOLATION: Profile switch mutated device grant secret!"
        }

        return true
    }

    fun clearActiveProfile() {
        activeProfileRef.set(null)
    }
}
