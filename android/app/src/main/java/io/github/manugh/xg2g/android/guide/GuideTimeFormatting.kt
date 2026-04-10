package io.github.manugh.xg2g.android.guide

import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter

internal fun GuideProgram.displayStartTime(displayZoneId: ZoneId): String =
    formatGuideProgramTime(startXmltv, startEpochSec, displayZoneId)

internal fun GuideProgram.displayEndTime(displayZoneId: ZoneId): String =
    formatGuideProgramTime(endXmltv, endEpochSec, displayZoneId)

internal fun formatGuideEpochTime(epochSec: Long, displayZoneId: ZoneId): String =
    GUIDE_TIME_FORMATTER.withZone(displayZoneId).format(Instant.ofEpochSecond(epochSec))

internal fun guideDisplayZoneId(offsetSeconds: Int?): ZoneId =
    offsetSeconds?.let(ZoneOffset::ofTotalSeconds) ?: ZoneId.systemDefault()

private fun formatGuideProgramTime(
    rawXmltvTime: String?,
    fallbackEpochSec: Long,
    fallbackZoneId: ZoneId
): String =
    parseXmltvOffsetDateTime(rawXmltvTime)?.format(GUIDE_TIME_FORMATTER)
        ?: formatGuideEpochTime(fallbackEpochSec, fallbackZoneId)

private fun parseXmltvOffsetDateTime(rawXmltvTime: String?): OffsetDateTime? {
    val normalized = rawXmltvTime?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    return runCatching {
        OffsetDateTime.parse(normalized, XMLTV_TIME_FORMATTER)
    }.getOrNull()
}

private val GUIDE_TIME_FORMATTER: DateTimeFormatter = DateTimeFormatter.ofPattern("HH:mm")
private val XMLTV_TIME_FORMATTER: DateTimeFormatter = DateTimeFormatter.ofPattern("yyyyMMddHHmmss Z")
