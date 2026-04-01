package io.github.manugh.xg2g.android.guide

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
    ): GuideContent {
        val bouquets = knownBouquets ?: apiClient.fetchBouquets(authToken)
        val selectedBouquet = when {
            bouquetName.isNotBlank() -> bouquetName
            bouquets.isNotEmpty() -> bouquets.first().name
            else -> ""
        }
        val channels = apiClient.fetchChannels(
            authToken = authToken,
            bouquetName = selectedBouquet.ifBlank { null }
        )
        val nowNext = apiClient.fetchNowNext(
            authToken = authToken,
            serviceRefs = channels.map(GuideChannel::serviceRef)
        )

        return GuideContent(
            bouquets = bouquets,
            selectedBouquet = selectedBouquet,
            channels = channels.map { channel ->
                val entry = nowNext[channel.serviceRef]
                channel.copy(
                    now = entry?.first,
                    next = entry?.second
                )
            }
        )
    }
}
