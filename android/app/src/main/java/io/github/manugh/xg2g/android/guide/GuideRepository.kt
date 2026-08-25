package io.github.manugh.xg2g.android.guide

import io.github.manugh.xg2g.android.transport.guide.GuideApiClient
import java.time.Instant
import kotlinx.coroutines.async
import kotlinx.coroutines.coroutineScope

internal interface GuideDataSource {
    suspend fun loadInitial(): GuideContent

    suspend fun loadBouquet(
        bouquetName: String,
        knownBouquets: List<GuideBouquet>? = null
    ): GuideContent
}

internal class GuideRepository(
    private val apiClient: GuideApiClient,
    private val authTokenProvider: () -> String?
) : GuideDataSource {
    private val currentToken: String? get() = authTokenProvider()

    override suspend fun loadInitial(): GuideContent {
        val token = currentToken
        val bouquets = apiClient.fetchBouquets(token)
        val selectedBouquet = bouquets.firstOrNull()?.name.orEmpty()
        return loadBouquet(selectedBouquet, bouquets)
    }

    override suspend fun loadBouquet(
        bouquetName: String,
        knownBouquets: List<GuideBouquet>?
    ): GuideContent = coroutineScope {
        val token = currentToken
        val deviceEpochSec = Instant.now().epochSecond
        val bouquetsDeferred = async {
            knownBouquets ?: apiClient.fetchBouquets(token)
        }
        val health = runCatching { apiClient.fetchHealthStatus(token) }.getOrNull()
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
                authToken = token,
                bouquetName = selectedBouquet.ifBlank { null }
            )
        }
        val scheduleDeferred = async {
            apiClient.fetchEpgWindow(
                authToken = token,
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
