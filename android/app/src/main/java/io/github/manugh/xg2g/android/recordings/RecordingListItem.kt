package io.github.manugh.xg2g.android.recordings

import androidx.compose.runtime.Immutable

@Immutable
data class RecordingResumeState(
    val posSeconds: Long = 0L,
    val durationSeconds: Long? = null,
    val finished: Boolean? = null,
    val updatedAt: String? = null
)

@Immutable
data class RecordingLibraryRoot(
    val id: String,
    val name: String
)

@Immutable
data class RecordingFolder(
    val name: String,
    val path: String
)

@Immutable
data class RecordingCrumb(
    val name: String,
    val path: String
)

@Immutable
data class RecordingListItem(
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
    val resume: RecordingResumeState? = null
)

@Immutable
data class RecordingsPage(
    val requestId: String? = null,
    val currentRoot: String? = null,
    val currentPath: String? = null,
    val roots: List<RecordingLibraryRoot> = emptyList(),
    val directories: List<RecordingFolder> = emptyList(),
    val breadcrumbs: List<RecordingCrumb> = emptyList(),
    val recordings: List<RecordingListItem> = emptyList()
)
