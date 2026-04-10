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
        val deviceEpochSec = Instant.now().epochSecond
        val bouquetsDeferred = async {
            knownBouquets ?: apiClient.fetchBouquets(authToken)
        }
        val health = runCatching { apiClient.fetchHealthStatus(authToken) }.getOrNull()
        val referenceEpochSec = health?.serverTimeEpochSec ?: deviceEpochSec
        val timelineWindow = buildGuideTimelineWindow(referenceEpochSec)
        val bouquets = bouquetsDeferred.await()
        val selectedBouquet = when {
            bouquetName.isNotBlank() -> bouquetName
            bouquets.isNotEmpty() -> bouquets.first().name
            else -> ""
        }
        val channelsDeferred = async {
            apiClient.fetchChannels(
                authToken = authToken,
                bouquetName = selectedBouquet.ifBlank { null }
            )
        }
        val scheduleDeferred = async {
            apiClient.fetchEpgWindow(
                authToken = authToken,
                bouquetName = selectedBouquet.ifBlank { null },
                timelineWindow = timelineWindow
            )
        }
        val channels = channelsDeferred.await()
        val scheduleByServiceRef = scheduleDeferred.await()

        GuideContent(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            channels = channels.map { channel ->
                val schedule = scheduleByServiceRef[canonicalGuideServiceRef(channel.serviceRef)]
                    .orEmpty()
                    .filter { it.overlaps(timelineWindow) }
                val entry = deriveGuideNowNext(schedule, referenceEpochSec)
                channel.copy(
                    now = entry.first,
                    next = entry.second,
                    schedule = schedule
                )
            },
            health = health,
            timelineWindow = timelineWindow,
            referenceEpochSec = referenceEpochSec,
            displayZoneOffsetSeconds = health?.serverTimeOffsetSeconds
        )
    }
}
