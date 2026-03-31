package io.github.manugh.xg2g.android.playback.net

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class NativePlaybackCapabilitiesTest {
    @Test
    fun `runtime probe exposes direct codecs and hardware signals for android tv`() {
        val capabilities = NativePlaybackCapabilities.fromMimeEntries(
            isTv = true,
            entries = listOf(
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.avc.decoder",
                    mimeType = "video/avc",
                    hardwareAccelerated = true,
                    maxWidth = 3840,
                    maxHeight = 2160,
                    maxFps = 60,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.hevc.decoder",
                    mimeType = "video/hevc",
                    hardwareAccelerated = true,
                    maxWidth = 3840,
                    maxHeight = 2160,
                    maxFps = 60,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.mpeg2.decoder",
                    mimeType = "video/mpeg2",
                    hardwareAccelerated = true,
                    maxWidth = 1920,
                    maxHeight = 1080,
                    maxFps = 60,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.ac3.decoder",
                    mimeType = "audio/ac3",
                    hardwareAccelerated = true,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.eac3.decoder",
                    mimeType = "audio/eac3",
                    hardwareAccelerated = true,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.aac.decoder",
                    mimeType = "audio/mp4a-latm",
                    hardwareAccelerated = true,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.mp2.decoder",
                    mimeType = "audio/mpeg-l2",
                    hardwareAccelerated = true,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.qti.mp3.decoder",
                    mimeType = "audio/mpeg",
                    hardwareAccelerated = true,
                ),
            )
        )

        assertEquals("android_tv", capabilities.deviceType)
        assertEquals(listOf("hls", "fmp4", "mpegts", "ts", "mp4"), capabilities.container)
        assertEquals(listOf("h264", "hevc", "mpeg2"), capabilities.videoCodecs)
        assertEquals(listOf("aac", "ac3", "eac3", "mp2", "mp3"), capabilities.audioCodecs)
        assertEquals(3840, capabilities.maxVideo?.width)
        assertEquals(2160, capabilities.maxVideo?.height)
        assertEquals(60, capabilities.maxVideo?.fps)
        assertTrue(capabilities.runtimeProbeUsed)
        assertEquals(2, capabilities.runtimeProbeVersion)

        val hevcSignal = capabilities.videoCodecSignals.first { it.codec == "hevc" }
        assertTrue(hevcSignal.supported)
        assertEquals(true, hevcSignal.powerEfficient)
        assertEquals(true, hevcSignal.smooth)
    }

    @Test
    fun `runtime probe keeps unsupported codecs visible as unsupported signals`() {
        val capabilities = NativePlaybackCapabilities.fromMimeEntries(
            isTv = false,
            entries = listOf(
                NativeDecoderMimeEntry(
                    codecName = "c2.android.avc.decoder",
                    mimeType = "video/avc",
                    hardwareAccelerated = false,
                    maxWidth = 1920,
                    maxHeight = 1080,
                    maxFps = 30,
                ),
                NativeDecoderMimeEntry(
                    codecName = "c2.android.aac.decoder",
                    mimeType = "audio/mp4a-latm",
                    hardwareAccelerated = false,
                ),
            )
        )

        assertEquals(listOf("h264"), capabilities.videoCodecs)
        assertEquals(listOf("aac"), capabilities.audioCodecs)
        assertEquals(false, capabilities.videoCodecSignals.first { it.codec == "h264" }.powerEfficient)
        assertEquals(false, capabilities.videoCodecSignals.first { it.codec == "av1" }.supported)
    }
}
