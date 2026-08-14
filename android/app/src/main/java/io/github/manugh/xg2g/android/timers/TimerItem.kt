package io.github.manugh.xg2g.android.timers

import androidx.compose.runtime.Immutable

@Immutable
internal data class TimerItem(
    val timerId: String,
    val title: String?,
    val serviceRef: String?,
    val serviceName: String?,
    val beginUnixSeconds: Long,
    val endUnixSeconds: Long,
    val state: String?,
    val disabled: Boolean,
    val justPlay: Boolean,
    val description: String? = null
)
