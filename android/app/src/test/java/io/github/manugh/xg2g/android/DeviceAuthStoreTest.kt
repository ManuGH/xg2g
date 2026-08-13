package io.github.manugh.xg2g.android

import android.content.SharedPreferences
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DeviceAuthStoreTest {

    private class FakeSharedPreferences(
        private val storage: MutableMap<String, Any?> = mutableMapOf(),
        private val shouldFailCommit: Boolean = false
    ) : SharedPreferences {

        override fun getAll(): MutableMap<String, *> = storage

        override fun getString(key: String, defValue: String?): String? {
            return storage[key] as? String ?: defValue
        }

        override fun getStringSet(key: String, defValues: MutableSet<String>?): MutableSet<String>? {
            @Suppress("UNCHECKED_CAST")
            return storage[key] as? MutableSet<String> ?: defValues
        }

        override fun getInt(key: String, defValue: Int): Int {
            return (storage[key] as? Number)?.toInt() ?: defValue
        }

        override fun getLong(key: String, defValue: Long): Long {
            return (storage[key] as? Number)?.toLong() ?: defValue
        }

        override fun getFloat(key: String, defValue: Float): Float {
            return (storage[key] as? Number)?.toFloat() ?: defValue
        }

        override fun getBoolean(key: String, defValue: Boolean): Boolean {
            return storage[key] as? Boolean ?: defValue
        }

        override fun contains(key: String): Boolean = storage.containsKey(key)

        override fun edit(): SharedPreferences.Editor = FakeEditor(storage, shouldFailCommit)

        override fun registerOnSharedPreferenceChangeListener(listener: SharedPreferences.OnSharedPreferenceChangeListener?) {}
        override fun unregisterOnSharedPreferenceChangeListener(listener: SharedPreferences.OnSharedPreferenceChangeListener?) {}

        private class FakeEditor(
            private val targetStorage: MutableMap<String, Any?>,
            private val failCommit: Boolean
        ) : SharedPreferences.Editor {

            private val changes = mutableMapOf<String, Any?>()
            private var clearPending = false

            override fun putString(key: String, value: String?): SharedPreferences.Editor {
                changes[key] = value
                return this
            }

            override fun putStringSet(key: String, values: MutableSet<String>?): SharedPreferences.Editor {
                changes[key] = values
                return this
            }

            override fun putInt(key: String, value: Int): SharedPreferences.Editor {
                changes[key] = value
                return this
            }

            override fun putLong(key: String, value: Long): SharedPreferences.Editor {
                changes[key] = value
                return this
            }

            override fun putFloat(key: String, value: Float): SharedPreferences.Editor {
                changes[key] = value
                return this
            }

            override fun putBoolean(key: String, value: Boolean): SharedPreferences.Editor {
                changes[key] = value
                return this
            }

            override fun remove(key: String): SharedPreferences.Editor {
                changes[key] = null
                return this
            }

            override fun clear(): SharedPreferences.Editor {
                clearPending = true
                changes.clear()
                return this
            }

            override fun commit(): Boolean {
                if (failCommit) {
                    return false
                }
                if (clearPending) {
                    targetStorage.clear()
                }
                changes.forEach { (key, value) ->
                    if (value == null) {
                        targetStorage.remove(key)
                    } else {
                        targetStorage[key] = value
                    }
                }
                return true
            }

            override fun apply() {
                commit()
            }
        }
    }

    @Test
    fun `successful migration copies legacy state and clears legacy store synchronously`() {
        val legacyStorage = mutableMapOf<String, Any?>(
            DeviceAuthStore.PREF_SERVER_URL to "https://xg2g.home.matrixcentral.de",
            DeviceAuthStore.PREF_DEVICE_GRANT_ID to "dgr_123456",
            DeviceAuthStore.PREF_DEVICE_GRANT to "grant_secret_abc",
            DeviceAuthStore.PREF_ACCESS_SESSION_ID to "sess_789",
            DeviceAuthStore.PREF_ACCESS_TOKEN to "tok_xyz",
            DeviceAuthStore.PREF_ACCESS_TOKEN_EXPIRES_AT_MS to 1800000000000L,
            DeviceAuthStore.PREF_POLICY_VERSION to "v1"
        )
        val legacyPrefs = FakeSharedPreferences(legacyStorage)
        val encryptedPrefs = FakeSharedPreferences(mutableMapOf())

        val result = DeviceAuthStore.migrateLegacyStoreIfNeeded(encryptedPrefs, legacyPrefs)

        assertTrue("Migration should return true on success", result)
        assertEquals("https://xg2g.home.matrixcentral.de", encryptedPrefs.getString(DeviceAuthStore.PREF_SERVER_URL, null))
        assertEquals("dgr_123456", encryptedPrefs.getString(DeviceAuthStore.PREF_DEVICE_GRANT_ID, null))
        assertEquals("grant_secret_abc", encryptedPrefs.getString(DeviceAuthStore.PREF_DEVICE_GRANT, null))
        assertEquals("sess_789", encryptedPrefs.getString(DeviceAuthStore.PREF_ACCESS_SESSION_ID, null))
        assertEquals("tok_xyz", encryptedPrefs.getString(DeviceAuthStore.PREF_ACCESS_TOKEN, null))
        assertEquals(1800000000000L, encryptedPrefs.getLong(DeviceAuthStore.PREF_ACCESS_TOKEN_EXPIRES_AT_MS, 0L))

        // Verify legacy store was cleared
        assertFalse("Legacy store must be cleared after successful migration", legacyPrefs.contains(DeviceAuthStore.PREF_DEVICE_GRANT_ID))
    }

    @Test
    fun `encrypted write failure retains legacy store intact`() {
        val legacyStorage = mutableMapOf<String, Any?>(
            DeviceAuthStore.PREF_SERVER_URL to "https://xg2g.home.matrixcentral.de",
            DeviceAuthStore.PREF_DEVICE_GRANT_ID to "dgr_123456",
            DeviceAuthStore.PREF_DEVICE_GRANT to "grant_secret_abc"
        )
        val legacyPrefs = FakeSharedPreferences(legacyStorage)
        val encryptedPrefsFailing = FakeSharedPreferences(mutableMapOf(), shouldFailCommit = true)

        val result = DeviceAuthStore.migrateLegacyStoreIfNeeded(encryptedPrefsFailing, legacyPrefs)

        assertFalse("Migration should return false on commit failure", result)
        assertEquals("dgr_123456", legacyPrefs.getString(DeviceAuthStore.PREF_DEVICE_GRANT_ID, null))
        assertEquals("grant_secret_abc", legacyPrefs.getString(DeviceAuthStore.PREF_DEVICE_GRANT, null))
    }

    @Test
    fun `migration is idempotent and does not run when legacy store has no grant`() {
        val emptyLegacyPrefs = FakeSharedPreferences(mutableMapOf())
        val encryptedPrefs = FakeSharedPreferences(mutableMapOf())

        val result = DeviceAuthStore.migrateLegacyStoreIfNeeded(encryptedPrefs, emptyLegacyPrefs)

        assertFalse("Migration should return false when legacy store is empty", result)
    }

    @Test
    fun `migration does not run when fallback store is active`() {
        val sharedPrefs = FakeSharedPreferences(mutableMapOf(
            DeviceAuthStore.PREF_DEVICE_GRANT_ID to "dgr_123"
        ))

        val result = DeviceAuthStore.migrateLegacyStoreIfNeeded(sharedPrefs, sharedPrefs)

        assertFalse("Migration should return false when encryptedPrefs === legacyPrefs", result)
    }
}
