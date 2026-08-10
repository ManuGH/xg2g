package io.github.manugh.xg2g.android.playback

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.joinAll
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Test

class PlaybackStateStoreTest {
    @Test
    fun `concurrent updates do not lose player generations`() = runBlocking {
        val store = PlaybackStateStore()
        val jobs = List(1_000) {
            launch(Dispatchers.Default) {
                store.update { state ->
                    state.copy(playerGeneration = state.playerGeneration + 1)
                }
            }
        }

        jobs.joinAll()

        assertEquals(1_000, store.current().playerGeneration)
    }
}
