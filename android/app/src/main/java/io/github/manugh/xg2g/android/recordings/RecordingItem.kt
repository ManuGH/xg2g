package io.github.manugh.xg2g.android.recordings

import androidx.compose.runtime.Immutable

@Immutable
data class ResumeSummary(
    val posSeconds: Long = 0L,
    val durationSeconds: Long? = null,
    val finished: Boolean? = null,
    val updatedAt: String? = null
)

@Immutable
data class RecordingRoot(
    val id: String,
    val name: String
)

@Immutable
data class DirectoryItem(
    val name: String,
    val path: String
)

@Immutable
data class Breadcrumb(
    val name: String,
    val path: String
)

@Immutable
data class RecordingItem(
    val recordingId: String,
    val serviceRef: String? = null,
    val title: String? = null,
    val description: String? = null,
    val beginUnixSeconds: Long? = null,
    val length: String? = null,
    val durationSeconds: Long? = null,
    val filename: String? = null,
    val status: String? = null,
    val localWritable: Boolean? = null,
    val resume: ResumeSummary? = null
)

@Immutable
data class RecordingsResponse(
    val requestId: String? = null,
    val currentRoot: String? = null,
    val currentPath: String? = null,
    val roots: List<RecordingRoot> = emptyList(),
    val directories: List<DirectoryItem> = emptyList(),
    val breadcrumbs: List<Breadcrumb> = emptyList(),
    val recordings: List<RecordingItem> = emptyList()
)
