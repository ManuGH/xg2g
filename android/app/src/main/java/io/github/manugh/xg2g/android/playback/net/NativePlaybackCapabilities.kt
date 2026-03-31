package io.github.manugh.xg2g.android.playback.net

import android.content.Context
import android.content.pm.PackageManager
import android.media.MediaCodecInfo
import android.media.MediaCodecList
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build

internal data class NativePlaybackCapabilities(
    val capabilitiesVersion: Int,
    val container: List<String>,
    val videoCodecs: List<String>,
    val videoCodecSignals: List<NativePlaybackVideoCodecSignal> = emptyList(),
    val audioCodecs: List<String>,
    val maxVideo: NativePlaybackMaxVideo? = null,
    val supportsHls: Boolean,
    val supportsRange: Boolean,
    val deviceType: String,
    val hlsEngines: List<String>,
    val preferredHlsEngine: String,
    val runtimeProbeUsed: Boolean,
    val runtimeProbeVersion: Int,
    val clientFamilyFallback: String,
    val allowTranscode: Boolean,
    val deviceContext: NativePlaybackDeviceContext? = null,
    val networkContext: NativePlaybackNetworkContext? = null,
) {
    companion object {
        private const val FEATURE_TELEVISION = "android.hardware.type.television"
        private const val RUNTIME_PROBE_VERSION = 2

        @Volatile
        private var cached: NativePlaybackCapabilities? = null

        fun create(context: Context): NativePlaybackCapabilities {
            cached?.let { return it }

            return synchronized(this) {
                cached?.let { return@synchronized it }

                val isTv = context.packageManager.hasSystemFeature(PackageManager.FEATURE_LEANBACK)
                    || context.packageManager.hasSystemFeature(FEATURE_TELEVISION)
                val capabilities = NativePlaybackCapabilityProbe.probe(context, isTv)
                cached = capabilities
                capabilities
            }
        }

        internal fun fromMimeEntries(
            isTv: Boolean,
            entries: List<NativeDecoderMimeEntry>
        ): NativePlaybackCapabilities {
            return NativePlaybackCapabilityProbe.fromMimeEntries(isTv, entries)
        }

        internal fun runtimeProbeVersion(): Int = RUNTIME_PROBE_VERSION
    }
}

internal data class NativePlaybackVideoCodecSignal(
    val codec: String,
    val supported: Boolean,
    val smooth: Boolean? = null,
    val powerEfficient: Boolean? = null,
)

internal data class NativePlaybackMaxVideo(
    val width: Int,
    val height: Int,
    val fps: Int? = null,
)

internal data class NativePlaybackDeviceContext(
    val brand: String? = null,
    val product: String? = null,
    val device: String? = null,
    val platform: String,
    val manufacturer: String? = null,
    val model: String? = null,
    val osName: String = "android",
    val osVersion: String? = null,
    val sdkInt: Int? = null,
)

internal data class NativePlaybackNetworkContext(
    val kind: String,
    val downlinkKbps: Int? = null,
    val metered: Boolean? = null,
    val internetValidated: Boolean? = null,
)

internal data class NativeDecoderMimeEntry(
    val codecName: String,
    val mimeType: String,
    val hardwareAccelerated: Boolean? = null,
    val maxWidth: Int? = null,
    val maxHeight: Int? = null,
    val maxFps: Int? = null,
)

private data class CodecTarget(
    val token: String,
    val mimeTypes: List<String>,
)

private object NativePlaybackCapabilityProbe {
    private const val DEVICE_ANDROID = "android"
    private const val DEVICE_ANDROID_TV = "android_tv"

    private val videoTargets = listOf(
        CodecTarget("av1", listOf("video/av01")),
        CodecTarget("hevc", listOf("video/hevc")),
        CodecTarget("h264", listOf("video/avc")),
        CodecTarget("mpeg2", listOf("video/mpeg2")),
        CodecTarget("vp9", listOf("video/x-vnd.on2.vp9")),
    )

    private val audioTargets = listOf(
        CodecTarget("aac", listOf("audio/mp4a-latm")),
        CodecTarget("ac3", listOf("audio/ac3")),
        CodecTarget("ac4", listOf("audio/ac4")),
        CodecTarget("eac3", listOf("audio/eac3", "audio/eac3-joc")),
        CodecTarget("mp2", listOf("audio/mpeg-l2", "audio/mp2")),
        CodecTarget("mp3", listOf("audio/mpeg")),
    )

    fun probe(context: Context, isTv: Boolean): NativePlaybackCapabilities {
        val entries = collectDecoderMimeEntries()
        return buildCapabilities(
            isTv = isTv,
            entries = entries,
            deviceContext = buildDeviceContext(isTv),
            networkContext = buildNetworkContext(context),
        )
    }

    fun fromMimeEntries(
        isTv: Boolean,
        entries: List<NativeDecoderMimeEntry>
    ): NativePlaybackCapabilities {
        return buildCapabilities(
            isTv = isTv,
            entries = entries,
            deviceContext = NativePlaybackDeviceContext(
                platform = if (isTv) "android-tv" else "android",
                osName = "android",
            ),
            networkContext = null,
        )
    }

    private fun buildCapabilities(
        isTv: Boolean,
        entries: List<NativeDecoderMimeEntry>,
        deviceContext: NativePlaybackDeviceContext?,
        networkContext: NativePlaybackNetworkContext?,
    ): NativePlaybackCapabilities {
        val videoSignals = videoTargets.map { target ->
            val matches = entries.filter { entry -> target.mimeTypes.any { mime -> mime.equals(entry.mimeType, ignoreCase = true) } }
            val maxVideo = mergeMaxVideo(matches)
            NativePlaybackVideoCodecSignal(
                codec = target.token,
                supported = matches.isNotEmpty(),
                smooth = maxVideo?.let { isLikelySmoothPlayback(it) },
                powerEfficient = resolvePowerEfficient(matches),
            )
        }

        val videoCodecs = videoSignals
            .filter { it.supported }
            .map { it.codec }
            .sorted()

        val audioCodecs = audioTargets
            .filter { target -> entries.any { entry -> target.mimeTypes.any { mime -> mime.equals(entry.mimeType, ignoreCase = true) } } }
            .map { it.token }
            .sorted()

        val maxVideo = mergeMaxVideo(
            entries.filter { entry -> videoTargets.any { target -> target.mimeTypes.any { mime -> mime.equals(entry.mimeType, ignoreCase = true) } } }
        )

        return NativePlaybackCapabilities(
            capabilitiesVersion = 3,
            container = listOf("hls", "fmp4", "mpegts", "ts", "mp4"),
            videoCodecs = videoCodecs,
            videoCodecSignals = videoSignals,
            audioCodecs = audioCodecs,
            maxVideo = maxVideo,
            supportsHls = true,
            supportsRange = true,
            deviceType = if (isTv) DEVICE_ANDROID_TV else DEVICE_ANDROID,
            hlsEngines = listOf("native"),
            preferredHlsEngine = "native",
            runtimeProbeUsed = true,
            runtimeProbeVersion = NativePlaybackCapabilities.runtimeProbeVersion(),
            clientFamilyFallback = if (isTv) "android_tv_native" else "android_native",
            allowTranscode = true,
            deviceContext = deviceContext,
            networkContext = networkContext,
        )
    }

    private fun collectDecoderMimeEntries(): List<NativeDecoderMimeEntry> {
        return runCatching {
            MediaCodecList(MediaCodecList.REGULAR_CODECS)
                .codecInfos
                .asSequence()
                .filterNot { it.isEncoder }
                .filterNot { shouldSkipCodec(it) }
                .flatMap { codecInfo ->
                    codecInfo.supportedTypes.asSequence().mapNotNull { mimeType ->
                        buildDecoderMimeEntry(codecInfo, mimeType)
                    }
                }
                .toList()
        }.getOrDefault(emptyList())
    }

    private fun shouldSkipCodec(codecInfo: MediaCodecInfo): Boolean {
        val name = codecInfo.name.lowercase()
        if (name.endsWith(".secure") || name.endsWith(".tunneled")) {
            return true
        }
        if (Build.VERSION.SDK_INT >= 29 && codecInfo.isAlias) {
            return true
        }
        return false
    }

    private fun buildDecoderMimeEntry(
        codecInfo: MediaCodecInfo,
        mimeType: String
    ): NativeDecoderMimeEntry? {
        val capabilities = runCatching { codecInfo.getCapabilitiesForType(mimeType) }.getOrNull() ?: return null
        val videoCapabilities = capabilities.videoCapabilities
        val maxWidth = videoCapabilities?.supportedWidths?.upper
        val maxHeight = videoCapabilities?.supportedHeights?.upper
        val maxFps = videoCapabilities
            ?.supportedFrameRates
            ?.upper
            ?.toString()
            ?.toDoubleOrNull()
            ?.let { fps ->
                if (fps >= 0) {
                    (fps + 0.5).toInt()
                } else {
                    (fps - 0.5).toInt()
                }
            }

        return NativeDecoderMimeEntry(
            codecName = codecInfo.name,
            mimeType = mimeType.lowercase(),
            hardwareAccelerated = isHardwareAccelerated(codecInfo),
            maxWidth = maxWidth,
            maxHeight = maxHeight,
            maxFps = maxFps,
        )
    }

    private fun isHardwareAccelerated(codecInfo: MediaCodecInfo): Boolean? {
        if (Build.VERSION.SDK_INT >= 29) {
            return codecInfo.isHardwareAccelerated
        }

        val name = codecInfo.name.lowercase()
        return when {
            name.startsWith("omx.google.") -> false
            name.startsWith("c2.android.") -> false
            name.startsWith("c2.google.") -> false
            else -> null
        }
    }

    private fun mergeMaxVideo(entries: List<NativeDecoderMimeEntry>): NativePlaybackMaxVideo? {
        val width = entries.mapNotNull { it.maxWidth }.maxOrNull() ?: return null
        val height = entries.mapNotNull { it.maxHeight }.maxOrNull() ?: return null
        val fps = entries.mapNotNull { it.maxFps }.maxOrNull()
        return NativePlaybackMaxVideo(width = width, height = height, fps = fps)
    }

    private fun resolvePowerEfficient(entries: List<NativeDecoderMimeEntry>): Boolean? {
        if (entries.isEmpty()) {
            return null
        }
        if (entries.any { it.hardwareAccelerated == true }) {
            return true
        }
        if (entries.all { it.hardwareAccelerated == false }) {
            return false
        }
        return null
    }

    private fun isLikelySmoothPlayback(maxVideo: NativePlaybackMaxVideo): Boolean {
        val fps = maxVideo.fps ?: return true
        return when {
            maxVideo.width >= 3840 && maxVideo.height >= 2160 -> fps >= 30
            maxVideo.width >= 1920 && maxVideo.height >= 1080 -> fps >= 50
            else -> fps >= 24
        }
    }

    private fun buildDeviceContext(isTv: Boolean): NativePlaybackDeviceContext {
        return NativePlaybackDeviceContext(
            brand = Build.BRAND.takeIf { it.isNotBlank() },
            product = Build.PRODUCT.takeIf { it.isNotBlank() },
            device = Build.DEVICE.takeIf { it.isNotBlank() },
            platform = if (isTv) "android-tv" else "android",
            manufacturer = Build.MANUFACTURER.takeIf { it.isNotBlank() },
            model = Build.MODEL.takeIf { it.isNotBlank() },
            osName = "android",
            osVersion = Build.VERSION.RELEASE?.takeIf { it.isNotBlank() },
            sdkInt = Build.VERSION.SDK_INT,
        )
    }

    private fun buildNetworkContext(context: Context): NativePlaybackNetworkContext? {
        return runCatching {
            val connectivityManager = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
                ?: return null
            val metered = runCatching { connectivityManager.isActiveNetworkMetered }.getOrNull()
            val activeNetwork = connectivityManager.activeNetwork
                ?: return NativePlaybackNetworkContext(
                    kind = "offline",
                    metered = metered,
                    internetValidated = false,
                )
            val networkCapabilities = connectivityManager.getNetworkCapabilities(activeNetwork)
                ?: return NativePlaybackNetworkContext(
                    kind = "unknown",
                    metered = metered,
                )

            NativePlaybackNetworkContext(
                kind = when {
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_BLUETOOTH) -> "bluetooth"
                    networkCapabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN) -> "vpn"
                    else -> "other"
                },
                downlinkKbps = networkCapabilities.linkDownstreamBandwidthKbps.takeIf { it > 0 },
                metered = metered,
                internetValidated = if (Build.VERSION.SDK_INT >= 23) {
                    networkCapabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
                } else {
                    networkCapabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                },
            )
        }.getOrNull()
    }
}
