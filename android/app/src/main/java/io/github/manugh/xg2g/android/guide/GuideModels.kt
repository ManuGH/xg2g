package io.github.manugh.xg2g.android.guide

internal data class GuideBouquet(
    val name: String,
    val services: Int = 0
)

internal data class GuideProgram(
    val title: String,
    val startEpochSec: Long,
    val endEpochSec: Long
)

internal data class GuideChannel(
    val serviceRef: String,
    val name: String,
    val number: String? = null,
    val group: String? = null,
    val logoUrl: String? = null,
    val resolution: String? = null,
    val codec: String? = null,
    val now: GuideProgram? = null,
    val next: GuideProgram? = null
) {
    val displayName: String
        get() = buildString {
            number?.takeIf { it.isNotBlank() }?.let {
                append(it)
                append(" · ")
            }
            append(name.ifBlank { serviceRef })
        }
}

internal data class GuideContent(
    val bouquets: List<GuideBouquet>,
    val selectedBouquet: String,
    val channels: List<GuideChannel>
)

internal sealed interface GuideScreenState {
    val serverLabel: String

    data class Loading(
        override val serverLabel: String
    ) : GuideScreenState

    data class Ready(
        override val serverLabel: String,
        val bouquets: List<GuideBouquet>,
        val selectedBouquet: String,
        val channels: List<GuideChannel>,
        val selectedChannelRef: String?,
        val isRefreshing: Boolean = false
    ) : GuideScreenState

    data class Empty(
        override val serverLabel: String,
        val bouquets: List<GuideBouquet>,
        val selectedBouquet: String
    ) : GuideScreenState

    data class Error(
        override val serverLabel: String,
        val detail: String,
        val authRequired: Boolean
    ) : GuideScreenState
}
