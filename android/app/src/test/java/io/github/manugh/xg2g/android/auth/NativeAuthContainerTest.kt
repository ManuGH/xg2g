package io.github.manugh.xg2g.android.auth

import android.content.Context
import android.content.ContextWrapper
import io.github.manugh.xg2g.android.PersistedDeviceAuthState
import io.github.manugh.xg2g.android.PersistedDeviceAuthStateStore
import io.github.manugh.xg2g.android.transport.auth.NativeDeviceAuthRepository
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertSame
import org.junit.Before
import org.junit.Test

class NativeAuthContainerTest {

    private class FakeContext : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this
        override fun getSystemService(name: String): Any? = null
    }

    private class FakeStore : PersistedDeviceAuthStateStore {
        override fun load(): PersistedDeviceAuthState? = null
        override fun save(state: PersistedDeviceAuthState) {}
        override fun clear() {}
    }

    private class FakeDPoP : DPoPProvider {
        override fun createProof(htm: String, htu: String, accessToken: String?): String = "proof"
        override fun getOrGenerateKeyPair(): KeyPair = KeyPairGenerator.getInstance("EC").apply { initialize(256) }.generateKeyPair()
        override fun getJWKThumbprint(): String = "jkt"
    }

    @Before
    @After
    fun resetContainer() {
        NativeAuthContainer.resetForTests()
    }

    @Test
    fun `getInstance returns singleton instance`() {
        val context = FakeContext()
        val container1 = NativeAuthContainer.getInstance(context, { FakeStore() }, { FakeDPoP() })
        val container2 = NativeAuthContainer.getInstance(context, { FakeStore() }, { FakeDPoP() })

        assertSame(container1, container2)
        assertNotNull(container1)
    }

    @Test
    fun `concurrent thread access yields exact same singleton dependencies`() {
        val context = FakeContext()
        val container = NativeAuthContainer.getInstance(context, { FakeStore() }, { FakeDPoP() })

        val executor = Executors.newFixedThreadPool(8)
        val stateMachines = ConcurrentHashMap.newKeySet<AuthStateMachine>()
        val dpopProviders = ConcurrentHashMap.newKeySet<DPoPProvider>()
        val repositories = ConcurrentHashMap.newKeySet<NativeDeviceAuthRepository>()

        val futures = (1..50).map {
            executor.submit {
                stateMachines.add(container.stateMachine)
                dpopProviders.add(container.dpopProvider)
                repositories.add(container.repository)
            }
        }
        futures.forEach { it.get() }
        executor.shutdown()

        assertEquals(1, stateMachines.size)
        assertEquals(1, dpopProviders.size)
        assertEquals(1, repositories.size)
    }
}
