package io.github.manugh.xg2g.android.guide

import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope
import java.time.Instant

internal class GuideRepository(
    private val apiClient: GuideApiClient,
    private val authToken: String?
) {
    suspend fun loadInitial(): GuideContent {
        val bouquets = apiClient.fetchBouquets(authToken)
        val selectedBouquet = bouquets.firstOrNull()?.name.orEmpty()
        return loadBouquet(selectedBouquet, bouquets)
    }

    suspend fun loadBouquet(
        bouquetName: String,
        knownBouquets: List<GuideBouquet>? = null
    ): GuideContent = coroutineScope {
        val currentEpochSec = Instant.now().epochSecond
        val timelineWindow = buildGuideTimelineWindow(currentEpochSec)
        val bouquets = knownBouquets ?: apiClient.fetchBouquets(authToken)
        val selectedBouquet = when {
            bouquetName.isNotBlank() -> bouquetName
            bouquets.isNotEmpty() -> bouquets.first().name
            else -> ""
        }
        val healthDeferred = async {
            runCatching { apiClient.fetchHealthStatus(authToken) }.getOrNull()
        }
        val channels = apiClient.fetchChannels(
            authToken = authToken,
            bouquetName = selectedBouquet.ifBlank { null }
        )
        val scheduleByServiceRef = apiClient.fetchEpgWindow(
            authToken = authToken,
            bouquetName = selectedBouquet.ifBlank { null },
            timelineWindow = timelineWindow
        )

        GuideContent(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            channels = channels.map { channel ->
                val schedule = scheduleByServiceRef[canonicalGuideServiceRef(channel.serviceRef)]
                    .orEmpty()
                    .filter { it.overlaps(timelineWindow) }
                val entry = deriveGuideNowNext(schedule, currentEpochSec)
                channel.copy(
                    now = entry.first,
                    next = entry.second,
                    schedule = schedule
                )
            },
            health = healthDeferred.await(),
            timelineWindow = timelineWindow
        )
    }
}
