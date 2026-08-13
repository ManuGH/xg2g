package io.github.manugh.xg2g.android.profile

import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.ServerSettingsStore
import io.github.manugh.xg2g.android.auth.SoftwareDPoPProvider
import io.github.manugh.xg2g.android.dashboard.NativeHouseholdProfile
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ProfileSecurityAndPolicyTest {

    private class FakeStore(var initial: PersistedDeviceAuthState? = null) : PersistedDeviceAuthStateStore {
        var saved: PersistedDeviceAuthState? = initial
        override fun load(): PersistedDeviceAuthState? = saved
        override fun save(state: PersistedDeviceAuthState) { saved = state }
        override fun clear() { saved = null }
    }

    private class FakeProfileSelectionStore : ProfileSelectionStore {
        var storedProfileId: String? = null
        override fun getSelectedProfileId(): String? = storedProfileId
        override fun saveSelectedProfileId(profileId: String?) { storedProfileId = profileId }
    }

    @Test
    fun `SECURITY INVARIANT Profile switch MUST NEVER mutate device grant or DPoP key`() {
        val deviceStore = FakeStore(
            PersistedDeviceAuthState(
                serverUrl = "https://xg2g.local",
                deviceGrantId = "dgr_immutable_100",
                deviceGrant = "grant_immutable_secret",
                accessToken = "at_dpop_100",
                accessTokenExpiresAtEpochMs = 120_000L
            )
        )
        val dpop = SoftwareDPoPProvider()
        val profileStore = FakeProfileSelectionStore()
        val profileManager = ProfileManager(profileStore, deviceStore, dpop)

        val childProfile = NativeHouseholdProfile(id = "prof_child", name = "Laura (Kind)", kind = "child", maxFsk = 6)
        val adultProfile = NativeHouseholdProfile(id = "prof_adult", name = "Manuel (Admin)", kind = "adult", maxFsk = 18)

        // 1. Switch to Child Profile
        profileManager.selectProfile(childProfile)
        assertEquals("prof_child", profileManager.activeProfileId)
        assertEquals("dgr_immutable_100", deviceStore.saved?.deviceGrantId)
        assertEquals("grant_immutable_secret", deviceStore.saved?.deviceGrant)

        // 2. Switch to Adult Profile
        profileManager.selectProfile(adultProfile)
        assertEquals("prof_adult", profileManager.activeProfileId)
        assertEquals("dgr_immutable_100", deviceStore.saved?.deviceGrantId)
        assertEquals("grant_immutable_secret", deviceStore.saved?.deviceGrant)
    }

    @Test
    fun `PinVerificationManager appends digits and validates 4-digit PIN`() {
        val pinManager = PinVerificationManager()
        assertEquals(0, pinManager.currentPinLength)
        assertFalse(pinManager.isComplete)

        pinManager.appendDigit(1)
        pinManager.appendDigit(2)
        pinManager.appendDigit(3)
        pinManager.appendDigit(4)

        assertTrue(pinManager.isComplete)
        assertTrue(pinManager.verifyPin("1234"))
        assertFalse(pinManager.verifyPin("9999"))
    }

    @Test
    fun `FskPolicyEvaluator evaluates content rating against profile limit`() {
        val evaluator = FskPolicyEvaluator()
        val childProfile = NativeHouseholdProfile(id = "prof_child", name = "Kind", kind = "child", maxFsk = 6)
        val adultProfile = NativeHouseholdProfile(id = "prof_adult", name = "Admin", kind = "adult", maxFsk = 18)

        // Child watches FSK 0 -> Permitted
        val res1 = evaluator.evaluate(contentFsk = 0, profile = childProfile)
        assertEquals(FskDecision.Permitted, res1)

        // Child watches FSK 16 -> ApprovalEligible
        val res2 = evaluator.evaluate(contentFsk = 16, profile = childProfile)
        assertTrue(res2 is FskDecision.ApprovalEligible)
        assertEquals(16, (res2 as FskDecision.ApprovalEligible).contentFsk)

        // Adult watches FSK 16 -> Permitted
        val res3 = evaluator.evaluate(contentFsk = 16, profile = adultProfile)
        assertEquals(FskDecision.Permitted, res3)
    }

    @Test
    fun `ApprovalRequestManager prevents duplicate submissions and updates on resolution`() {
        val manager = ApprovalRequestManager(nowEpochMs = { 10_000L })
        val resourceId = "recording_movie_fsk16"

        // 1. Initial Submit -> Pending
        val sub1 = manager.submitRequest(resourceId, requestIdGenerator = { "req_100" })
        assertTrue(sub1 is ApprovalStatus.Pending)
        assertEquals("req_100", (sub1 as ApprovalStatus.Pending).requestId)

        // 2. Duplicate Submit -> Returns existing Pending request
        val sub2 = manager.submitRequest(resourceId, requestIdGenerator = { "req_duplicate" })
        assertTrue(sub2 is ApprovalStatus.Pending)
        assertEquals("req_100", (sub2 as ApprovalStatus.Pending).requestId)

        // 3. Admin Approves via FCM / WebUI -> Status changes to Approved
        val res = manager.resolveRequest(resourceId, requestId = "req_100", approved = true)
        assertTrue(res is ApprovalStatus.Approved)
        assertEquals(ApprovalStatus.Approved("req_100", resourceId), manager.getStatus(resourceId))
    }
}
