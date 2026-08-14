package io.github.manugh.xg2g.android.guide

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.awaitCancellation
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class GuideViewModelTest {
    @Test
    fun `refresh cancellation never replaces loading state with an error`() = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        try {
            val replacement = CompletableDeferred<GuideContent>()
            val source = object : GuideDataSource {
                var calls = 0

                override suspend fun loadInitial(): GuideContent {
                    calls += 1
                    if (calls == 1) {
                        awaitCancellation()
                    }
                    return replacement.await()
                }

                override suspend fun loadBouquet(
                    bouquetName: String,
                    knownBouquets: List<GuideBouquet>?
                ): GuideContent = error("not used")
            }
            val viewModel = GuideViewModel("xg2g", source)
            runCurrent()

            viewModel.refresh()
            runCurrent()

            assertFalse(viewModel.state.value is GuideScreenState.Error)
            assertTrue(viewModel.state.value is GuideScreenState.Loading)

            replacement.complete(
                GuideContent(
                    bouquets = emptyList(),
                    selectedBouquet = "",
                    channels = emptyList(),
                    referenceEpochSec = 1L
                )
            )
            advanceUntilIdle()
            assertTrue(viewModel.state.value is GuideScreenState.Empty)
        } finally {
            Dispatchers.resetMain()
        }
    }
}
