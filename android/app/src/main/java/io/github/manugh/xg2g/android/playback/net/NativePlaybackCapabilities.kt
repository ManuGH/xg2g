package io.github.manugh.xg2g.android.playback.net

import android.content.Context
import android.content.pm.PackageManager

internal data class NativePlaybackCapabilities(
    val capabilitiesVersion: Int,
    val container: List<String>,
    val videoCodecs: List<String>,
    val audioCodecs: List<String>,
    val supportsHls: Boolean,
    val supportsRange: Boolean,
    val deviceType: String,
    val hlsEngines: List<String>,
    val preferredHlsEngine: String,
    val runtimeProbeUsed: Boolean,
    val runtimeProbeVersion: Int,
    val clientFamilyFallback: String,
    val allowTranscode: Boolean,
) {
    companion object {
        private const val FEATURE_TELEVISION = "android.hardware.type.television"

        fun create(context: Context): NativePlaybackCapabilities {
            val isTv = context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
                || context.packageManager.hasSystemFeature(FEATURE_TELEVISION)

            return NativePlaybackCapabilities(
                capabilitiesVersion = 3,
                container = listOf("hls", "mpegts", "ts", "mp4"),
                videoCodecs = listOf("h264"),
                audioCodecs = listOf("aac", "mp3", "ac3"),
                supportsHls = true,
                supportsRange = true,
                deviceType = if (isTv) "android_tv" else "android",
                hlsEngines = listOf("native"),
                preferredHlsEngine = "native",
                runtimeProbeUsed = false,
                runtimeProbeVersion = 1,
                clientFamilyFallback = if (isTv) "android_tv_native" else "android_native",
                allowTranscode = true,
            )
        }
    }
}
