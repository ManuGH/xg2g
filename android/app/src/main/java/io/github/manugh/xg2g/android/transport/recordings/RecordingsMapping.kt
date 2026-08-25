package io.github.manugh.xg2g.android.transport.recordings

import io.github.manugh.xg2g.android.recordings.RecordingCrumb
import io.github.manugh.xg2g.android.recordings.RecordingFolder
import io.github.manugh.xg2g.android.recordings.RecordingLibraryRoot
import io.github.manugh.xg2g.android.recordings.RecordingListItem
import io.github.manugh.xg2g.android.recordings.RecordingResumeState
import io.github.manugh.xg2g.android.recordings.RecordingsPage
import io.github.manugh.xg2g.android.contract.Breadcrumb as WireBreadcrumb
import io.github.manugh.xg2g.android.contract.DirectoryItem as WireDirectoryItem
import io.github.manugh.xg2g.android.contract.RecordingItem as WireRecordingItem
import io.github.manugh.xg2g.android.contract.RecordingResponse as WireRecordingResponse
import io.github.manugh.xg2g.android.contract.RecordingRoot as WireRecordingRoot
import io.github.manugh.xg2g.android.contract.ResumeSummary as WireResumeSummary

// The mapping between the generated wire contract and the types the recordings
// UI works with.
//
// It exists because the two are shaped by different things. The wire types are
// shaped by the schema, so almost every field is optional — the schema requires
// only `status` on an item, and a recording without a `recordingId` is
// representable there. The UI types are shaped by what a list row needs, so
// `recordingId` is not optional: a row the user cannot open is not a row.
//
// Reconciling the two is a decision, and this file is where it is made once
// rather than at each call site. Items the wire allows but the UI cannot use
// are dropped here, which is why several of these return null.

internal fun WireRecordingResponse.toDomain(): RecordingsPage = RecordingsPage(
    requestId = requestId.takeIf { it.isNotBlank() },
    currentRoot = currentRoot?.takeIf { it.isNotBlank() },
    currentPath = currentPath?.takeIf { it.isNotBlank() },
    roots = roots?.mapNotNull { it.toDomain() }.orEmpty(),
    directories = directories?.mapNotNull { it.toDomain() }.orEmpty(),
    breadcrumbs = breadcrumbs?.mapNotNull { it.toDomain() }.orEmpty(),
    recordings = recordings?.mapNotNull { it.toDomain() }.orEmpty()
)

/** A root without an id cannot be navigated to, so it is not offered. */
internal fun WireRecordingRoot.toDomain(): RecordingLibraryRoot? {
    val rootId = id?.takeIf { it.isNotBlank() } ?: return null
    return RecordingLibraryRoot(id = rootId, name = name?.takeIf { it.isNotBlank() } ?: rootId)
}

internal fun WireDirectoryItem.toDomain(): RecordingFolder? {
    val folderName = name?.takeIf { it.isNotBlank() } ?: return null
    return RecordingFolder(name = folderName, path = path?.takeIf { it.isNotBlank() }.orEmpty())
}

internal fun WireBreadcrumb.toDomain(): RecordingCrumb? {
    val crumbName = name?.takeIf { it.isNotBlank() } ?: return null
    return RecordingCrumb(name = crumbName, path = path?.takeIf { it.isNotBlank() }.orEmpty())
}

internal fun WireResumeSummary.toDomain(): RecordingResumeState = RecordingResumeState(
    posSeconds = posSeconds,
    durationSeconds = durationSeconds,
    finished = finished,
    // The wire carries an instant; the row only ever renders it.
    updatedAt = updatedAt?.toString()
)

/** A recording the UI cannot address is dropped rather than shown. */
internal fun WireRecordingItem.toDomain(): RecordingListItem? {
    val id = recordingId?.takeIf { it.isNotBlank() } ?: return null
    return RecordingListItem(
        recordingId = id,
        serviceRef = serviceRef?.takeIf { it.isNotBlank() },
        title = title?.takeIf { it.isNotBlank() } ?: "Aufnahme",
        description = description?.takeIf { it.isNotBlank() },
        beginUnixSeconds = beginUnixSeconds,
        length = length?.takeIf { it.isNotBlank() },
        durationSeconds = durationSeconds,
        filename = filename?.takeIf { it.isNotBlank() },
        status = status.wireValue,
        localWritable = localWritable,
        resume = resume?.toDomain()
    )
}
